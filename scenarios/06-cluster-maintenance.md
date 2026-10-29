---
title: Cluster Inspection & Maintenance
description: Node info, taints and tolerations, Kustomize, CRDs and ResourceQuotas.
difficulty: medium
domains: Cluster Architecture, Workloads & Scheduling (25%)
duration: 45
pass: 66
---

# Cluster Inspection & Maintenance

The cluster has a single node (see `kubectl get nodes`). Save any requested files under `/tmp/cka`.

## Setup

```bash
kubectl create namespace arch
mkdir -p /tmp/cka/kustomize/base
cat > /tmp/cka/kustomize/base/deployment.yaml <<YAML
apiVersion: apps/v1
kind: Deployment
metadata:
  name: web
spec:
  replicas: 1
  selector:
    matchLabels: {app: web}
  template:
    metadata:
      labels: {app: web}
    spec:
      containers:
      - {name: nginx, image: "nginx:1.25"}
YAML
cat > /tmp/cka/kustomize/base/kustomization.yaml <<YAML
resources:
- deployment.yaml
YAML
cat <<YAML | kubectl apply -f -
apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  name: widgets.cka.example.com
spec:
  group: cka.example.com
  scope: Namespaced
  names: {plural: widgets, singular: widget, kind: Widget}
  versions:
  - name: v1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
        properties:
          spec:
            type: object
            properties:
              size: {type: integer}
YAML
kubectl wait --for=condition=Established crd/widgets.cka.example.com --timeout=60s
```

## Cleanup

```bash
kubectl delete namespace arch limited --ignore-not-found --timeout=180s
kubectl delete crd widgets.cka.example.com --ignore-not-found --timeout=60s
for n in $(kubectl get nodes -o name); do kubectl taint $n dedicated- >/dev/null 2>&1; done
true
```

## Task 1 - Node information (15%)

Write the following into files:

- `/tmp/cka/nodes.txt`: the name of the node(s), one per line
- `/tmp/cka/kubelet-version.txt`: the kubelet version of the node (e.g. `v1.30.2+k3s1`)
- `/tmp/cka/runtime.txt`: the container runtime of the node (e.g. `containerd://1.7.15`)

### Solution

```bash
kubectl get nodes -o jsonpath='{.items[*].metadata.name}' | tr ' ' '\n' > /tmp/cka/nodes.txt
kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.kubeletVersion}' > /tmp/cka/kubelet-version.txt
kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.containerRuntimeVersion}' > /tmp/cka/runtime.txt
```

### Verify

```bash
eq "$(sort /tmp/cka/nodes.txt | tr -d ' ' | grep -v '^$' | tr '\n' ' ')" "$(kubectl get nodes -o jsonpath='{.items[*].metadata.name}' | tr ' ' '\n' | sort | tr '\n' ' ')" "nodes.txt"
eq "$(tr -d '[:space:]' < /tmp/cka/kubelet-version.txt)" "$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.kubeletVersion}')" "kubelet-version.txt"
eq "$(tr -d '[:space:]' < /tmp/cka/runtime.txt)" "$(kubectl get nodes -o jsonpath='{.items[0].status.nodeInfo.containerRuntimeVersion}')" "runtime.txt"
echo OK
```

## Task 2 - Taints and tolerations (25%)

- Taint the node with `dedicated=batch:NoSchedule`
- In namespace `arch` create Pod `batch-pod` (image `busybox:1.36`, command `sleep 3600`) that tolerates this taint; it must run
- In namespace `arch` create Pod `normal-pod` (image `busybox:1.36`, command `sleep 3600`) **without** any toleration. It must stay `Pending` because of the taint.

### Solution

```bash
NODE=$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')
kubectl taint node $NODE dedicated=batch:NoSchedule
kubectl -n arch run normal-pod --image=busybox:1.36 -- sleep 3600
kubectl -n arch run batch-pod --image=busybox:1.36 $do -- sleep 3600 > batch.yaml
# add:  tolerations: [{key: dedicated, operator: Equal, value: batch, effect: NoSchedule}]
kubectl apply -f batch.yaml
```

### Verify

```bash
NODE=$(kubectl get nodes -o jsonpath='{.items[0].metadata.name}')
eq "$(kubectl get node $NODE -o jsonpath='{.spec.taints[?(@.key=="dedicated")].value}:{.spec.taints[?(@.key=="dedicated")].effect}')" "batch:NoSchedule" "node taint"
kubectl -n arch wait --for=condition=Ready pod/batch-pod --timeout=90s || fail "batch-pod not ready"
eq "$(kubectl -n arch get pod batch-pod -o jsonpath='{.spec.tolerations[?(@.key=="dedicated")].value}')" "batch" "toleration"
eq "$(kubectl -n arch get pod normal-pod -o jsonpath='{.status.phase}')" "Pending" "normal-pod must be Pending"
echo OK
```

## Task 3 - Kustomize overlay (25%)

A base is available in `/tmp/cka/kustomize/base`. Create an overlay in `/tmp/cka/kustomize/overlay` that

- uses the base
- sets the namespace to `arch`
- prefixes all names with `prod-`
- sets replicas of Deployment `web` to `3`

Apply the overlay to the cluster. (Its Pods may stay `Pending` if the node is tainted from Task 2 - that's fine here.)

### Solution

```bash
mkdir -p /tmp/cka/kustomize/overlay && cd /tmp/cka/kustomize/overlay
cat > kustomization.yaml <<YAML
resources:
- ../base
namespace: arch
namePrefix: prod-
replicas:
- name: web
  count: 3
YAML
kubectl apply -k .
```

### Verify

```bash
[ -f /tmp/cka/kustomize/overlay/kustomization.yaml ] || fail "overlay/kustomization.yaml missing"
eq "$(kubectl -n arch get deploy prod-web -o jsonpath='{.spec.replicas}')" "3" "replicas"
echo OK
```

## Task 4 - Custom Resources (20%)

- Write the names of all CRDs belonging to the group `cka.example.com` into `/tmp/cka/crds.txt`
- Create a `Widget` named `w1` in namespace `arch` with `spec.size: 3`

### Solution

```bash
kubectl get crd -o name | grep cka.example.com | sed 's#.*/##' > /tmp/cka/crds.txt
kubectl apply -f - <<YAML
apiVersion: cka.example.com/v1
kind: Widget
metadata: {name: w1, namespace: arch}
spec: {size: 3}
YAML
```

### Verify

```bash
eq "$(tr -d '[:space:]' < /tmp/cka/crds.txt)" "widgets.cka.example.com" "crds.txt"
eq "$(kubectl -n arch get widget w1 -o jsonpath='{.spec.size}')" "3" "widget size"
echo OK
```

## Task 5 - ResourceQuota (15%)

Create a namespace `limited` and a ResourceQuota `q` in it that allows at most `3` Pods and `5` ConfigMaps.

### Solution

```bash
kubectl create namespace limited
kubectl -n limited create quota q --hard=pods=3,configmaps=5
```

### Verify

```bash
eq "$(kubectl -n limited get resourcequota q -o jsonpath='{.spec.hard.pods}')" "3" "pods"
eq "$(kubectl -n limited get resourcequota q -o jsonpath='{.spec.hard.configmaps}')" "5" "configmaps"
echo OK
```
