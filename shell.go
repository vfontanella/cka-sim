package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"syscall"
	"time"

	"github.com/creack/pty"
	"github.com/gorilla/websocket"
)

const helpers = `fail(){ echo "FAIL: $*"; exit 1; }
eq(){ [ "$1" = "$2" ] || fail "$3 (expected '$2', got '$1')"; }
contains(){ case "$1" in *"$2"*) ;; *) fail "$3 (expected to contain '$2', got '$1')";; esac; }
`

// runScript runs a bash script with a timeout, killing the whole process group on expiry.
func runScript(script string, env []string, dir string, timeout time.Duration) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "/bin/bash", "-c", script)
	cmd.Env = append(os.Environ(), env...)
	cmd.Dir = dir
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL) }
	cmd.WaitDelay = 2 * time.Second
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		code = -1
		if ee, ok := err.(*exec.ExitError); ok {
			code = ee.ExitCode()
		}
		if ctx.Err() == context.DeadlineExceeded {
			return string(out) + "\n[timed out after " + timeout.String() + "]", 124
		}
	}
	return string(out), code
}

const scrollbackMax = 256 * 1024

const bashrc = `alias k=kubectl
export do="--dry-run=client -o yaml"
export now="--force --grace-period 0"
export EDITOR=vi KUBE_EDITOR=vi
export HISTFILE=%s
PS1='\[\e[32m\]candidate@cka-sim\[\e[0m\]:\[\e[34m\]\w\[\e[0m\]\$ '
`

// Shell is a persistent interactive bash running on a pty. Output is kept in a scrollback buffer so the
// browser can reattach after a reload; a shell that exits is restarted.
type Shell struct {
	mu         sync.Mutex
	dir        string
	work       string
	kubeconfig string
	ptmx       *os.File
	cmd        *exec.Cmd
	client     *websocket.Conn
	scrollback []byte
	closed     bool
}

func NewShell(kubeconfig, work string) (*Shell, error) {
	dir, err := os.MkdirTemp("", "cka-shell-")
	if err != nil {
		return nil, err
	}
	rc := fmt.Sprintf(bashrc, filepath.Join(dir, "history"))
	if err := os.WriteFile(filepath.Join(dir, "bashrc"), []byte(rc), 0600); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	s := &Shell{dir: dir, work: work, kubeconfig: kubeconfig}
	if err := s.spawn(); err != nil {
		os.RemoveAll(dir)
		return nil, err
	}
	return s, nil
}

func (s *Shell) spawn() error {
	cmd := exec.Command("/bin/bash", "--rcfile", filepath.Join(s.dir, "bashrc"), "-i")
	cmd.Dir = s.work
	cmd.Env = append(os.Environ(), "KUBECONFIG="+s.kubeconfig, "TERM=xterm-256color", "BASH_SILENCE_DEPRECATION_WARNING=1")
	ptmx, err := pty.StartWithSize(cmd, &pty.Winsize{Rows: 30, Cols: 100})
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.ptmx, s.cmd = ptmx, cmd
	s.mu.Unlock()
	go s.pump(ptmx, cmd)
	return nil
}

func (s *Shell) pump(ptmx *os.File, cmd *exec.Cmd) {
	buf := make([]byte, 8192)
	for {
		n, err := ptmx.Read(buf)
		if n > 0 {
			s.mu.Lock()
			s.scrollback = append(s.scrollback, buf[:n]...)
			if len(s.scrollback) > scrollbackMax {
				s.scrollback = append([]byte(nil), s.scrollback[len(s.scrollback)-scrollbackMax:]...)
			}
			if s.client != nil {
				s.client.WriteMessage(websocket.BinaryMessage, buf[:n])
			}
			s.mu.Unlock()
		}
		if err != nil {
			break
		}
	}
	cmd.Wait()
	ptmx.Close()
	s.mu.Lock()
	closed := s.closed
	s.mu.Unlock()
	if !closed {
		s.write("\r\n[shell exited - restarting]\r\n")
		s.spawn()
	}
}

func (s *Shell) write(msg string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scrollback = append(s.scrollback, msg...)
	if s.client != nil {
		s.client.WriteMessage(websocket.BinaryMessage, []byte(msg))
	}
}

func (s *Shell) Close() {
	s.mu.Lock()
	s.closed = true
	if s.client != nil {
		s.client.Close()
	}
	if s.cmd != nil && s.cmd.Process != nil {
		syscall.Kill(-s.cmd.Process.Pid, syscall.SIGHUP)
		s.cmd.Process.Kill()
	}
	if s.ptmx != nil {
		s.ptmx.Close()
	}
	s.mu.Unlock()
	os.RemoveAll(s.dir)
}

var upgrader = websocket.Upgrader{ReadBufferSize: 4096, WriteBufferSize: 4096}

// Attach serves one browser connection: binary frames are raw terminal input, text frames are JSON control messages.
func (s *Shell) Attach(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	s.mu.Lock()
	if s.client != nil {
		s.client.Close()
	}
	s.client = conn
	conn.WriteMessage(websocket.BinaryMessage, s.scrollback)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.client == conn {
			s.client = nil
		}
		s.mu.Unlock()
		conn.Close()
	}()
	for {
		typ, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		s.mu.Lock()
		ptmx := s.ptmx
		s.mu.Unlock()
		if typ == websocket.TextMessage {
			var m struct {
				Type       string `json:"type"`
				Cols, Rows uint16
			}
			if json.Unmarshal(data, &m) == nil && m.Type == "resize" && m.Cols > 0 && m.Rows > 0 {
				pty.Setsize(ptmx, &pty.Winsize{Cols: m.Cols, Rows: m.Rows})
			}
		} else {
			ptmx.Write(data)
		}
	}
}
