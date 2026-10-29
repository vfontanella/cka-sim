package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

type Task struct {
	Number   int    `json:"number"`
	Title    string `json:"title"`
	Weight   int    `json:"weight"`
	Body     string `json:"body"`
	Solution string `json:"-"`
	Verify   string `json:"-"`
}

type Scenario struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	Description  string `json:"description"`
	Difficulty   string `json:"difficulty"`
	Domains      string `json:"domains"`
	DurationMin  int    `json:"durationMin"`
	PassScore    int    `json:"passScore"`
	Instructions string `json:"instructions"`
	Tasks        []Task `json:"-"`
	TaskCount    int    `json:"taskCount"`
	Setup        string `json:"-"`
	Cleanup      string `json:"-"`
}

var taskHeading = regexp.MustCompile(`^##\s+Task\s+(\d+)\s*[:\-–]\s*(.+?)(?:\s*\((\d+)%\))?\s*$`)

// ParseScenario parses the markdown scenario format documented in README.md.
func ParseScenario(id string, data []byte) (*Scenario, error) {
	lines := strings.Split(strings.ReplaceAll(string(data), "\r\n", "\n"), "\n")
	s := &Scenario{ID: id, DurationMin: 60, PassScore: 66}
	i := 0
	if len(lines) > 0 && strings.TrimSpace(lines[0]) == "---" {
		for i = 1; i < len(lines) && strings.TrimSpace(lines[i]) != "---"; i++ {
			k, v, ok := strings.Cut(lines[i], ":")
			if !ok {
				continue
			}
			v = strings.TrimSpace(v)
			switch strings.ToLower(strings.TrimSpace(k)) {
			case "title":
				s.Title = v
			case "description":
				s.Description = v
			case "difficulty":
				s.Difficulty = v
			case "domains":
				s.Domains = v
			case "duration":
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					s.DurationMin = n
				}
			case "pass":
				if n, err := strconv.Atoi(v); err == nil && n > 0 {
					s.PassScore = n
				}
			}
		}
		i++
	}

	section, sub := "intro", "body"
	var cur *Task
	var intro []string
	var body, solution []string
	inFence := false
	var codeSink *strings.Builder
	var setup, cleanup, verify strings.Builder

	flushTask := func() {
		if cur == nil {
			return
		}
		cur.Body = strings.TrimSpace(strings.Join(body, "\n"))
		cur.Solution = strings.TrimSpace(strings.Join(solution, "\n"))
		cur.Verify = verify.String()
		verify.Reset()
		s.Tasks = append(s.Tasks, *cur)
		cur, body, solution = nil, nil, nil
	}

	for ; i < len(lines); i++ {
		line := lines[i]
		trim := strings.TrimSpace(line)
		isFence := strings.HasPrefix(trim, "```")

		if !inFence {
			if m := taskHeading.FindStringSubmatch(line); m != nil {
				flushTask()
				n, _ := strconv.Atoi(m[1])
				w := 0
				if m[3] != "" {
					w, _ = strconv.Atoi(m[3])
				}
				cur = &Task{Number: n, Title: m[2], Weight: w}
				section, sub = "task", "body"
				continue
			}
			if strings.HasPrefix(line, "## ") {
				flushTask()
				switch strings.ToLower(strings.TrimSpace(line[3:])) {
				case "setup":
					section = "setup"
				case "cleanup":
					section = "cleanup"
				default:
					section = "ignore"
				}
				continue
			}
			if section == "task" && strings.HasPrefix(line, "### ") {
				switch strings.ToLower(strings.TrimSpace(line[4:])) {
				case "verify":
					sub = "verify"
				case "solution":
					sub = "solution"
				default:
					sub = "body"
				}
				continue
			}
			if section == "intro" && strings.HasPrefix(line, "# ") {
				if s.Title == "" {
					s.Title = strings.TrimSpace(line[2:])
				}
				continue
			}
		}

		if isFence {
			inFence = !inFence
			if inFence {
				switch {
				case section == "setup":
					codeSink = &setup
				case section == "cleanup":
					codeSink = &cleanup
				case section == "task" && sub == "verify":
					codeSink = &verify
				default:
					codeSink = nil
				}
			} else {
				wasSink := codeSink != nil
				codeSink = nil
				if wasSink {
					continue
				}
			}
			if inFence && codeSink != nil {
				continue
			}
		} else if inFence && codeSink != nil {
			codeSink.WriteString(line + "\n")
			continue
		}

		switch section {
		case "intro":
			intro = append(intro, line)
		case "task":
			switch sub {
			case "body":
				body = append(body, line)
			case "solution":
				solution = append(solution, line)
			}
		}
	}
	flushTask()

	s.Setup, s.Cleanup = setup.String(), cleanup.String()
	s.Instructions = strings.TrimSpace(strings.Join(intro, "\n"))
	if len(s.Tasks) == 0 {
		return nil, fmt.Errorf("no tasks found")
	}
	total, unset := 0, 0
	for _, t := range s.Tasks {
		total += t.Weight
		if t.Weight == 0 {
			unset++
		}
	}
	if unset > 0 { // distribute leftover evenly
		share := 1
		if left := 100 - total; left > 0 {
			share = left / unset
		}
		for k := range s.Tasks {
			if s.Tasks[k].Weight == 0 {
				s.Tasks[k].Weight = share
			}
		}
	}
	if s.Title == "" {
		s.Title = id
	}
	s.TaskCount = len(s.Tasks)
	return s, nil
}

func LoadScenario(dir, id string) (*Scenario, error) {
	if id != filepath.Base(id) || strings.HasPrefix(id, ".") {
		return nil, fmt.Errorf("invalid scenario id")
	}
	data, err := os.ReadFile(filepath.Join(dir, id+".md"))
	if err != nil {
		return nil, err
	}
	return ParseScenario(id, data)
}

func ListScenarios(dir string) ([]*Scenario, []string) {
	files, _ := filepath.Glob(filepath.Join(dir, "*.md"))
	sort.Strings(files)
	var out []*Scenario
	var errs []string
	for _, f := range files {
		id := strings.TrimSuffix(filepath.Base(f), ".md")
		if strings.EqualFold(id, "README") {
			continue
		}
		s, err := LoadScenario(dir, id)
		if err != nil {
			errs = append(errs, id+": "+err.Error())
			continue
		}
		out = append(out, s)
	}
	return out, errs
}
