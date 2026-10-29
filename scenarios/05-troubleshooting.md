---
title: Troubleshooting
description: Fix broken Deployments, Services, scheduling problems and missing configuration; collect logs.
difficulty: hard
domains: Troubleshooting (30%)
duration: 45
pass: 66
---

# Troubleshooting

Everything happens in namespace `trouble`. Save any requested files under `/tmp/cka`.

## Setup

```bash
kubectl create namespace trouble

kubectl -n trouble create deployment frontend --image=nginx:1.99.99 --replicas=2

kubectl -n trouble create deployment backend --image=nginx:1.25 --replicas=2
kubectl -n trouble expose deployment backend --name=backend-svc --port=80
kubectl -n trouble patch service backend-svc -p '{"spec":{"selector":{"app":"backend-v2"}}}'

kubectl -n trouble create deployment heavy --image=nginx:1.25
kubectl -n trouble set resources deployment heavy --requests=cpu=64,memory=512Gi

cat <<YAML | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata: {name: config-pod, namespace: trouble}
spec:
  containers:
  - name: app
    image: busybox:1.36
    command: [sh, -c, "echo level=\$LEVEL; sleep 3600"]
    env:
    - name: LEVEL
      valueFrom:
        configMapKeyRef: {name: missing-cfg, key: LEVEL}
---
apiVersion: v1
kind: Pod
metadata: {name: logger, namespace: trouble}
spec:
  containers:
  - name: logger
    image: busybox:1.36
    command:
    - sh
    - -c
    - 'for i in 1 2 3; do echo "INFO line \$i"; echo "ERROR failure \$i"; done; sleep 3600'
YAML
kubectl -n trouble wait --for=condition=Ready pod/logger --timeout=120s
kubectl -n trouble rollout status deployment/backend --timeout=120s
```

## Cleanup

```bash
kubectl delete namespace trouble --ignore-not-found --timeout=180s
true
```

## Task 1 - ImagePullBackOff (20%)

The Deployment `frontend` in namespace `trouble` has Pods that never start. Fix the Deployment so that both replicas are available (use `nginx:1.25`).

### Solution

```bash
kubectl -n trouble describe pod -l app=frontend | grep -i -A3 events   # image not found
kubectl -n trouble set image deployment/frontend nginx=nginx:1.25
```

### Verify

```bash
kubectl -n trouble rollout status deploy/frontend --timeout=120s || fail "rollout not complete"
eq "$(kubectl -n trouble get deploy frontend -o jsonpath='{.status.availableReplicas}')" "2" "available replicas"
eq "$(kubectl -n trouble get deploy frontend -o jsonpath='{.spec.replicas}')" "2" "replicas"
echo OK
```

## Task 2 - Service without endpoints (20%)

The Service `backend-svc` doesn't route traffic to the Deployment `backend`. Fix the **Service** (don't modify the Deployment or its Pods).

### Solution

```bash
kubectl -n trouble get endpoints backend-svc          # <none>
kubectl -n trouble get pods --show-labels
kubectl -n trouble patch svc backend-svc -p '{"spec":{"selector":{"app":"backend"}}}'
```

### Verify

```bash
eq "$(kubectl -n trouble get deploy backend -o jsonpath='{.spec.template.metadata.labels.app}')" "backend" "deployment labels must be untouched"
eq "$(kubectl -n trouble get svc backend-svc -o jsonpath='{.spec.selector.app}')" "backend" "service selector"
eq "$(kubectl -n trouble get deploy backend -o jsonpath='{.metadata.generation}')" "1" "deployment must not be modified"
n=$(kubectl -n trouble get endpoints backend-svc -o jsonpath='{.subsets[0].addresses[*].ip}' | wc -w | tr -d ' ')
eq "$n" "2" "number of endpoints"
echo OK
```

## Task 3 - Pending Pod (20%)

The Deployment `heavy` has a Pod that stays `Pending`. Find out why and change the Deployment's container resources so the Pod runs: requests `cpu: 100m`, `memory: 64Mi`.

### Solution

```bash
kubectl -n trouble describe pod -l app=heavy | tail     # Insufficient cpu / memory
kubectl -n trouble set resources deployment heavy --requests=cpu=100m,memory=64Mi
```

### Verify

```bash
kubectl -n trouble rollout status deploy/heavy --timeout=120s || fail "rollout not complete"
eq "$(kubectl -n trouble get deploy heavy -o jsonpath='{.spec.template.spec.containers[0].resources.requests.cpu}')" "100m" "cpu request"
eq "$(kubectl -n trouble get deploy heavy -o jsonpath='{.spec.template.spec.containers[0].resources.requests.memory}')" "64Mi" "memory request"
eq "$(kubectl -n trouble get deploy heavy -o jsonpath='{.status.availableReplicas}')" "1" "available replicas"
echo OK
```

## Task 4 - Missing ConfigMap (20%)

The Pod `config-pod` is stuck. Without recreating the Pod, make it start by creating whatever is missing: the ConfigMap must provide key `LEVEL` with value `debug`.

### Solution

```bash
kubectl -n trouble describe pod config-pod | tail    # configmap "missing-cfg" not found
kubectl -n trouble create configmap missing-cfg --from-literal=LEVEL=debug
```

### Verify

```bash
eq "$(kubectl -n trouble get cm missing-cfg -o jsonpath='{.data.LEVEL}')" "debug" "configmap value"
kubectl -n trouble wait --for=condition=Ready pod/config-pod --timeout=120s || fail "config-pod not ready"
contains "$(kubectl -n trouble logs config-pod)" "level=debug" "pod log"
echo OK
```

## Task 5 - Collect error logs (20%)

The Pod `logger` writes both INFO and ERROR lines. Save **only** the lines containing `ERROR` to `/tmp/cka/errors.txt`.

### Solution

```bash
kubectl -n trouble logs logger | grep ERROR > /tmp/cka/errors.txt
```

### Verify

```bash
[ -f /tmp/cka/errors.txt ] || fail "file missing"
eq "$(wc -l < /tmp/cka/errors.txt | tr -d ' ')" "3" "number of lines"
eq "$(grep -c ERROR /tmp/cka/errors.txt)" "3" "ERROR lines"
eq "$(grep -c INFO /tmp/cka/errors.txt)" "0" "INFO lines"
echo OK
```
