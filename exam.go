package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"time"
)

const workDir = "/tmp/cka"

type TaskResult struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Weight   int    `json:"weight"`
	Passed   bool   `json:"passed"`
	Output   string `json:"output"`
	Solution string `json:"solution"`
}

type Result struct {
	Score     int          `json:"score"`
	PassScore int          `json:"passScore"`
	Passed    bool         `json:"passed"`
	Tasks     []TaskResult `json:"tasks"`
}

type Exam struct {
	Scenario *Scenario
	Started  time.Time
	Shell    *Shell
	Result   *Result
}

type ExamManager struct {
	mu      sync.Mutex
	cluster *Cluster
	exam    *Exam
}

func NewExamManager(c *Cluster) *ExamManager { return &ExamManager{cluster: c} }

func (m *ExamManager) resetWork() {
	os.RemoveAll(workDir)
	os.MkdirAll(workDir, 0755)
}

func (m *ExamManager) script(body string, timeout time.Duration) (string, int) {
	return runScript(helpers+body, m.cluster.env(), workDir, timeout)
}

func (m *ExamManager) Start(s *Scenario) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.exam != nil && m.exam.Result == nil {
		return fmt.Errorf("an exam is already in progress")
	}
	if st := m.cluster.Status(); !st.Ready {
		return fmt.Errorf("the Kubernetes cluster is not ready - start it first")
	}
	m.resetWork()
	if m.exam != nil && m.exam.Shell != nil {
		m.exam.Shell.Close()
	}
	m.exam = nil
	m.script(s.Cleanup, 5*time.Minute)
	m.script(`kubectl taint nodes --all dedicated- >/dev/null 2>&1; for n in $(kubectl get nodes -o name); do kubectl uncordon $n >/dev/null 2>&1; done; true`, time.Minute)
	if out, code := m.script(s.Setup, 5*time.Minute); code != 0 {
		m.script(s.Cleanup, 5*time.Minute)
		return fmt.Errorf("scenario setup failed (exit %d):\n%s", code, out)
	}
	sh, err := NewShell(m.cluster.Kubeconfig(), workDir)
	if err != nil {
		return err
	}
	m.exam = &Exam{Scenario: s, Started: time.Now(), Shell: sh}
	return nil
}

func (m *ExamManager) Current() *Exam {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.exam
}

func (m *ExamManager) Finish() (*Result, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	e := m.exam
	if e == nil {
		return nil, fmt.Errorf("no exam in progress")
	}
	if e.Result != nil {
		return e.Result, nil
	}
	res := &Result{PassScore: e.Scenario.PassScore}
	total, got := 0, 0
	for _, t := range e.Scenario.Tasks {
		out, code := m.script(t.Verify, 90*time.Second)
		tr := TaskResult{Number: t.Number, Title: t.Title, Weight: t.Weight, Passed: code == 0,
			Output: strings.TrimSpace(out), Solution: t.Solution}
		total += t.Weight
		if tr.Passed {
			got += t.Weight
		}
		res.Tasks = append(res.Tasks, tr)
	}
	if total > 0 {
		res.Score = got * 100 / total
	}
	res.Passed = res.Score >= res.PassScore
	e.Result = res
	e.Shell.Close()
	go func(cleanup string) {
		m.mu.Lock()
		defer m.mu.Unlock()
		m.script(cleanup, 5*time.Minute)
	}(e.Scenario.Cleanup)
	return res, nil
}

func (m *ExamManager) Abort() {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.exam == nil {
		return
	}
	if m.exam.Result == nil {
		m.exam.Shell.Close()
		m.script(m.exam.Scenario.Cleanup, 5*time.Minute)
	}
	m.exam = nil
}
