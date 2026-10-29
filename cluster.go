package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ClusterConfig struct {
	Profile    string
	CPUs       int
	Memory     int
	Disk       int
	K8sVersion string
	K3sPort    int
	DataDir    string
}

type Cluster struct {
	cfg ClusterConfig

	mu      sync.Mutex
	busy    bool
	op      string
	log     []string
	lastErr string
}

type ClusterStatus struct {
	ColimaInstalled  bool     `json:"colimaInstalled"`
	KubectlInstalled bool     `json:"kubectlInstalled"`
	Running          bool     `json:"running"`
	Ready            bool     `json:"ready"`
	Busy             bool     `json:"busy"`
	Operation        string   `json:"operation"`
	Profile          string   `json:"profile"`
	Nodes            string   `json:"nodes"`
	Error            string   `json:"error"`
	Log              []string `json:"log"`
}

func NewCluster(cfg ClusterConfig) *Cluster { return &Cluster{cfg: cfg} }

func (c *Cluster) Kubeconfig() string { return filepath.Join(c.cfg.DataDir, "kubeconfig") }
func (c *Cluster) context() string    { return "colima-" + c.cfg.Profile }
func (c *Cluster) env() []string      { return []string{"KUBECONFIG=" + c.Kubeconfig()} }

func have(bin string) bool { _, err := exec.LookPath(bin); return err == nil }

func (c *Cluster) logf(format string, a ...any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.log = append(c.log, fmt.Sprintf(format, a...))
	if len(c.log) > 500 {
		c.log = c.log[len(c.log)-500:]
	}
}

func (c *Cluster) Status() ClusterStatus {
	st := ClusterStatus{ColimaInstalled: have("colima"), KubectlInstalled: have("kubectl"), Profile: c.cfg.Profile}
	c.mu.Lock()
	st.Busy, st.Operation, st.Error = c.busy, c.op, c.lastErr
	st.Log = append([]string(nil), c.log...)
	c.mu.Unlock()
	if !st.ColimaInstalled || !st.KubectlInstalled || st.Busy {
		return st
	}
	if _, code := runScript("colima status -p "+c.cfg.Profile, nil, "", 15*time.Second); code == 0 {
		st.Running = true
		if _, err := os.Stat(c.Kubeconfig()); err != nil {
			c.exportKubeconfig()
		}
		out, code := runScript("kubectl get nodes --no-headers", c.env(), "", 15*time.Second)
		if code == 0 && strings.Contains(out, " Ready") {
			st.Ready = true
			st.Nodes = strings.TrimSpace(out)
		}
	}
	return st
}

func (c *Cluster) exportKubeconfig() error {
	home, _ := os.UserHomeDir()
	out, code := runScript(fmt.Sprintf("kubectl --kubeconfig %q config view --raw --minify --context %s",
		filepath.Join(home, ".kube", "config"), c.context()), nil, "", 20*time.Second)
	if code != 0 {
		return fmt.Errorf("cannot export kubeconfig for context %s: %s", c.context(), strings.TrimSpace(out))
	}
	if err := os.MkdirAll(c.cfg.DataDir, 0700); err != nil {
		return err
	}
	return os.WriteFile(c.Kubeconfig(), []byte(out), 0600)
}

// Do runs a long-lived operation (start|stop|delete) in the background.
func (c *Cluster) Do(op string) error {
	c.mu.Lock()
	if c.busy {
		c.mu.Unlock()
		return fmt.Errorf("another operation (%s) is in progress", c.op)
	}
	c.busy, c.op, c.lastErr, c.log = true, op, "", nil
	c.mu.Unlock()
	go func() {
		err := c.run(op)
		c.mu.Lock()
		c.busy, c.op = false, ""
		if err != nil {
			c.lastErr = err.Error()
		}
		c.mu.Unlock()
		if err != nil {
			c.logf("ERROR: %v", err)
		} else {
			c.logf("done")
		}
	}()
	return nil
}

func (c *Cluster) stream(name string, args ...string) error {
	c.logf("$ %s %s", name, strings.Join(args, " "))
	cmd := exec.Command(name, args...)
	pr, pw := io.Pipe()
	cmd.Stdout, cmd.Stderr = pw, pw
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan struct{})
	go func() {
		sc := bufio.NewScanner(pr)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			c.logf("%s", sc.Text())
		}
		close(done)
	}()
	err := cmd.Wait()
	pw.Close()
	<-done
	return err
}

func (c *Cluster) run(op string) error {
	p := c.cfg.Profile
	switch op {
	case "start":
		if !have("colima") || !have("kubectl") {
			return fmt.Errorf("colima and kubectl must be installed (brew install colima kubectl docker)")
		}
		args := []string{"start", "-p", p, "--kubernetes",
			"--cpu", fmt.Sprint(c.cfg.CPUs), "--memory", fmt.Sprint(c.cfg.Memory), "--disk", fmt.Sprint(c.cfg.Disk)}
		if c.cfg.K3sPort != 0 {
			args = append(args, "--k3s-listen-port", fmt.Sprint(c.cfg.K3sPort))
		}
		if c.cfg.K8sVersion != "" {
			args = append(args, "--kubernetes-version", c.cfg.K8sVersion)
		}
		if err := c.stream("colima", args...); err != nil {
			return fmt.Errorf("colima start failed: %w", err)
		}
		if err := c.exportKubeconfig(); err != nil {
			return err
		}
		c.logf("waiting for the node to be Ready...")
		for i := 0; i < 90; i++ {
			out, code := runScript("kubectl get nodes --no-headers", c.env(), "", 15*time.Second)
			if code == 0 && strings.Contains(out, " Ready") {
				c.logf("%s", strings.TrimSpace(out))
				return nil
			}
			time.Sleep(2 * time.Second)
		}
		return fmt.Errorf("timed out waiting for node to become Ready")
	case "stop":
		return c.stream("colima", "stop", "-p", p)
	case "delete":
		os.Remove(c.Kubeconfig())
		return c.stream("colima", "delete", "-p", p, "--force")
	}
	return fmt.Errorf("unknown operation %q", op)
}
