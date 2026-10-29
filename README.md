# CKA Exam Simulator

A Go app that serves a web UI imitating the CKA exam (question pane, terminal, countdown timer, flags, notepad, grading)
against a real Kubernetes cluster that it creates for you with [Colima](https://github.com/abiosoft/colima) (k3s).
Scenarios are plain markdown files in `scenarios/`.

## Requirements

`go`, `colima`, `kubectl`, `docker` (`brew install colima kubectl docker`). Nothing else - the Go app has no dependencies.

## Run

```bash
go run .            # or: go build -o cka-sim . && ./cka-sim
```

Flags: `-addr 127.0.0.1:8080`, `-scenarios ./scenarios`, `-profile cka-sim`, `-cpus 4`, `-memory 6`, `-disk 30`, `-k8s-version vX.Y.Z+k3s1`, `-k3s-port 16443`, `-open=false`.

1. Open the page and click **Start cluster** (runs `colima start -p cka-sim --kubernetes ...`). Your `~/.kube/config` is only
   read; the simulator uses its own kubeconfig at `~/.cka-sim/kubeconfig`.
2. Pick a scenario. Its setup script runs against the cluster, then the timer starts.
3. Work in the terminal: a real interactive bash on a pty (xterm.js over a WebSocket), running on your Mac with
   `KUBECONFIG` preset; `k`, `$do`, `$now` available; working dir `/tmp/cka`; `vi`, `kubectl edit`, `exec -it` all work.
   The session survives page reloads.
4. **End exam** (or timer expiry) runs each task's verify script, shows score, per-task output and solutions, then cleans up.

Scenarios: 01 Workloads & Scheduling, 02 Services & Networking, 03 Storage, 04 RBAC & Security, 05 Troubleshooting, 06 Cluster Inspection & Maintenance.
Colima's Kubernetes is a single-node k3s, so kubeadm-only topics (cluster upgrade, etcd backup) are not covered.

## Scenario format

````markdown
---
title: My scenario
description: Shown on the card
difficulty: easy|medium|hard
domains: free text
duration: 40        # minutes
pass: 66            # pass percentage
---
Intro text shown before the exam starts.

## Setup
```bash
# hidden; runs before the exam (after Cleanup)
```

## Cleanup
```bash
# hidden; runs before setup and after the exam; must be idempotent
```

## Task 1 - Title (20%)
Task text (markdown).

### Solution
Markdown shown in the results.

### Verify
```bash
# exit 0 = task passed. Helpers: fail MSG, eq ACTUAL EXPECTED MSG, contains STR SUBSTR MSG
eq "$(kubectl get ...)" "expected" "what was checked"
```
````

Weights should sum to 100. Setup/Verify/Cleanup run with `KUBECONFIG` set and `/tmp/cka` as cwd (wiped at exam start).
