---
title: Workloads & Scheduling
description: Deployments, rollbacks, node selectors, CronJobs, ConfigMaps and resource limits.
difficulty: easy
domains: Workloads & Scheduling (15%)
duration: 40
pass: 66
---

# Workloads & Scheduling

All resources live in the namespace `shop` unless stated otherwise. Save any requested files under `/tmp/cka`.

## Setup

```bash
kubectl create namespace shop
kubectl -n shop create deployment web --image=nginx:1.24 --replicas=2
kubectl -n shop create deployment broken-app --image=nginx:1.25
kubectl -n shop rollout status deployment/broken-app --timeout=120s
kubectl -n shop set image deployment/broken-app nginx=nginx:doesnotexist-99
kubectl -n shop create configmap app-config --from-literal=APP_MODE=production --from-literal=LOG_LEVEL=info
kubectl -n shop rollout status deployment/web --timeout=120s
```

## Cleanup

```bash
kubectl delete namespace shop --ignore-not-found --timeout=180s
for n in $(kubectl get nodes -o name); do kubectl label $n disk- >/dev/null 2>&1; done
true
```

## Task 1 - Update and scale a Deployment (20%)

In namespace `shop`, the Deployment `web` currently runs `nginx:1.24`.

- Update its container image to `nginx:1.25`
- Scale it to `4` replicas
- Make sure the rollout completes successfully

### Solution

```bash
kubectl -n shop set image deployment/web nginx=nginx:1.25
kubectl -n shop scale deployment/web --replicas=4
kubectl -n shop rollout status deployment/web
```

### Verify

```bash
eq "$(kubectl -n shop get deploy web -o jsonpath='{.spec.template.spec.containers[0].image}')" "nginx:1.25" "image"
eq "$(kubectl -n shop get deploy web -o jsonpath='{.spec.replicas}')" "4" "spec.replicas"
kubectl -n shop rollout status deploy/web --timeout=90s || fail "rollout not complete"
eq "$(kubectl -n shop get deploy web -o jsonpath='{.status.availableReplicas}')" "4" "available replicas"
echo OK
```

## Task 2 - Roll back a broken Deployment (15%)

The Deployment `broken-app` in namespace `shop` was updated with an image that does not exist, so new pods can't start.
Roll the Deployment back to its previous working revision.

### Solution

```bash
kubectl -n shop rollout undo deployment/broken-app
kubectl -n shop rollout status deployment/broken-app
```

### Verify

```bash
img=$(kubectl -n shop get deploy broken-app -o jsonpath='{.spec.template.spec.containers[0].image}')
eq "$img" "nginx:1.25" "image after rollback"
kubectl -n shop rollout status deploy/broken-app --timeout=90s || fail "rollout not complete"
echo OK
```

## Task 3 - Schedule a Pod using a node label (20%)

- Label the cluster node(s) with `disk=ssd`
- Create a Pod named `ssd-pod` in namespace `shop` using image `redis:7` that must be scheduled only on nodes labelled `disk=ssd` (use a `nodeSelector`)
- The Pod must be running

### Solution

```bash
for n in $(kubectl get nodes -o name); do kubectl label $n disk=ssd; done
kubectl -n shop run ssd-pod --image=redis:7 $do > ssd.yaml
# add under spec:   nodeSelector: { disk: ssd }
kubectl apply -f ssd.yaml
```

### Verify

```bash
[ -n "$(kubectl get nodes -l disk=ssd -o name)" ] || fail "no node labelled disk=ssd"
eq "$(kubectl -n shop get pod ssd-pod -o jsonpath='{.spec.nodeSelector.disk}')" "ssd" "nodeSelector"
kubectl -n shop wait --for=condition=Ready pod/ssd-pod --timeout=90s || fail "pod not ready"
echo OK
```

## Task 4 - Create a CronJob (20%)

Create a CronJob named `cleanup` in namespace `shop` that:

- runs every 10 minutes (`*/10 * * * *`)
- uses image `busybox:1.36` and runs the command `sh -c "echo cleaning"`
- keeps `2` successful and `1` failed finished jobs in history

### Solution

```bash
kubectl -n shop create cronjob cleanup --image=busybox:1.36 --schedule="*/10 * * * *" -- sh -c "echo cleaning"
kubectl -n shop patch cronjob cleanup -p '{"spec":{"successfulJobsHistoryLimit":2,"failedJobsHistoryLimit":1}}'
```

### Verify

```bash
J() { kubectl -n shop get cronjob cleanup -o jsonpath="$1"; }
eq "$(J '{.spec.schedule}')" "*/10 * * * *" "schedule"
eq "$(J '{.spec.jobTemplate.spec.template.spec.containers[0].image}')" "busybox:1.36" "image"
eq "$(J '{.spec.successfulJobsHistoryLimit}')" "2" "successfulJobsHistoryLimit"
eq "$(J '{.spec.failedJobsHistoryLimit}')" "1" "failedJobsHistoryLimit"
cmd="$(J '{.spec.jobTemplate.spec.template.spec.containers[0].command}{.spec.jobTemplate.spec.template.spec.containers[0].args}')"
contains "$cmd" "echo cleaning" "command"
echo OK
```

## Task 5 - Pod with config and resource limits (25%)

Create a Pod named `cfg-pod` in namespace `shop`:

- image `busybox:1.36`, command `sleep 3600`
- all keys of the existing ConfigMap `app-config` must be exposed as environment variables
- resource requests: cpu `50m`, memory `32Mi`
- resource limits: cpu `100m`, memory `64Mi`

### Solution

```bash
kubectl -n shop run cfg-pod --image=busybox:1.36 $do -- sleep 3600 > cfg.yaml
# add to the container:
#   envFrom: [{configMapRef: {name: app-config}}]
#   resources: {requests: {cpu: 50m, memory: 32Mi}, limits: {cpu: 100m, memory: 64Mi}}
kubectl apply -f cfg.yaml
```

### Verify

```bash
P() { kubectl -n shop get pod cfg-pod -o jsonpath="$1"; }
eq "$(P '{.spec.containers[0].envFrom[0].configMapRef.name}')" "app-config" "envFrom configMapRef"
eq "$(P '{.spec.containers[0].resources.requests.cpu}')" "50m" "requests.cpu"
eq "$(P '{.spec.containers[0].resources.requests.memory}')" "32Mi" "requests.memory"
eq "$(P '{.spec.containers[0].resources.limits.cpu}')" "100m" "limits.cpu"
eq "$(P '{.spec.containers[0].resources.limits.memory}')" "64Mi" "limits.memory"
kubectl -n shop wait --for=condition=Ready pod/cfg-pod --timeout=90s || fail "pod not ready"
eq "$(kubectl -n shop exec cfg-pod -- printenv APP_MODE)" "production" "APP_MODE env"
echo OK
```
