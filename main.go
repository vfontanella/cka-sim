package main

import (
	"embed"
	"encoding/json"
	"flag"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"time"
)

//go:embed web
var webFS embed.FS

type Server struct {
	scenarioDir string
	cluster     *Cluster
	exams       *ExamManager
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func httpErr(w http.ResponseWriter, code int, err error) {
	writeJSON(w, code, map[string]string{"error": err.Error()})
}

func (s *Server) examView() any {
	e := s.exams.Current()
	if e == nil {
		return map[string]any{"active": false}
	}
	return map[string]any{
		"active":     true,
		"scenario":   e.Scenario,
		"tasks":      e.Scenario.Tasks,
		"startedAt":  e.Started.UnixMilli(),
		"durationMs": e.Scenario.DurationMin * 60 * 1000,
		"now":        time.Now().UnixMilli(),
		"result":     e.Result,
	}
}

func (s *Server) routes() http.Handler {
	mux := http.NewServeMux()
	sub, _ := fs.Sub(webFS, "web")
	mux.Handle("/", http.FileServer(http.FS(sub)))

	mux.HandleFunc("GET /api/cluster", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.cluster.Status())
	})
	mux.HandleFunc("POST /api/cluster/{op}", func(w http.ResponseWriter, r *http.Request) {
		op := r.PathValue("op")
		if op != "start" && op != "stop" && op != "delete" {
			httpErr(w, 400, fmt.Errorf("unknown operation"))
			return
		}
		if e := s.exams.Current(); e != nil && e.Result == nil && op != "start" {
			httpErr(w, 409, fmt.Errorf("an exam is in progress"))
			return
		}
		if err := s.cluster.Do(op); err != nil {
			httpErr(w, 409, err)
			return
		}
		writeJSON(w, 202, map[string]string{"status": "started"})
	})
	mux.HandleFunc("GET /api/scenarios", func(w http.ResponseWriter, r *http.Request) {
		list, errs := ListScenarios(s.scenarioDir)
		writeJSON(w, 200, map[string]any{"scenarios": list, "errors": errs})
	})
	mux.HandleFunc("GET /api/exam", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, s.examView())
	})
	mux.HandleFunc("POST /api/exam/start", func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ID string `json:"id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpErr(w, 400, err)
			return
		}
		sc, err := LoadScenario(s.scenarioDir, req.ID)
		if err != nil {
			httpErr(w, 404, err)
			return
		}
		if err := s.exams.Start(sc); err != nil {
			httpErr(w, 409, err)
			return
		}
		writeJSON(w, 200, s.examView())
	})
	mux.HandleFunc("POST /api/exam/finish", func(w http.ResponseWriter, r *http.Request) {
		if _, err := s.exams.Finish(); err != nil {
			httpErr(w, 409, err)
			return
		}
		writeJSON(w, 200, s.examView())
	})
	mux.HandleFunc("POST /api/exam/abort", func(w http.ResponseWriter, r *http.Request) {
		s.exams.Abort()
		writeJSON(w, 200, map[string]bool{"ok": true})
	})
	mux.HandleFunc("GET /api/term", func(w http.ResponseWriter, r *http.Request) {
		e := s.exams.Current()
		if e == nil || e.Result != nil {
			httpErr(w, 409, fmt.Errorf("no exam in progress"))
			return
		}
		e.Shell.Attach(w, r)
	})
	return mux
}

func main() {
	addr := flag.String("addr", "127.0.0.1:8080", "listen address")
	dir := flag.String("scenarios", "scenarios", "directory containing scenario markdown files")
	profile := flag.String("profile", "cka-sim", "colima profile name")
	cpus := flag.Int("cpus", 4, "colima CPUs")
	mem := flag.Int("memory", 6, "colima memory (GiB)")
	disk := flag.Int("disk", 30, "colima disk (GiB)")
	k8s := flag.String("k8s-version", "", "kubernetes version for colima (default: colima's default)")
	k3sPort := flag.Int("k3s-port", 16443, "fixed k3s API port (colima otherwise picks a random one per start, which breaks k3s restarts)")
	open := flag.Bool("open", true, "open the browser on start")
	flag.Parse()

	home, _ := os.UserHomeDir()
	cl := NewCluster(ClusterConfig{Profile: *profile, CPUs: *cpus, Memory: *mem, Disk: *disk,
		K8sVersion: *k8s, K3sPort: *k3sPort, DataDir: filepath.Join(home, ".cka-sim")})
	srv := &Server{scenarioDir: *dir, cluster: cl, exams: NewExamManager(cl)}

	url := "http://" + *addr
	log.Printf("CKA simulator listening on %s (scenarios: %s)", url, *dir)
	if *open {
		go func() {
			time.Sleep(500 * time.Millisecond)
			opener := "xdg-open"
			if runtime.GOOS == "darwin" {
				opener = "open"
			}
			exec.Command(opener, url).Start()
		}()
	}
	log.Fatal(http.ListenAndServe(*addr, srv.routes()))
}
