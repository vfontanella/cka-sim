---
title: Storage
description: PersistentVolumes, PersistentClaims, dynamic provisioning with the default StorageClass, and shared volumes.
difficulty: medium
domains: Storage (10%)
duration: 35
pass: 66
---

# Storage

Everything happens in namespace `storage` unless stated otherwise. Save any requested files under `/tmp/cka`.

## Setup

```bash
kubectl create namespace storage
```

## Cleanup

```bash
kubectl delete namespace storage --ignore-not-found --timeout=180s
kubectl delete pv pv-data --ignore-not-found --timeout=60s
true
```

## Task 1 - PersistentVolume (15%)

Create a PersistentVolume named `pv-data`:

- capacity `1Gi`, access mode `ReadWriteOnce`
- `storageClassName: manual`
- `hostPath` of `/mnt/pv-data`
- reclaim policy `Retain`

### Solution

```yaml
apiVersion: v1
kind: PersistentVolume
metadata:
  name: pv-data
spec:
  capacity:
    storage: 1Gi
  accessModes: [ReadWriteOnce]
  storageClassName: manual
  persistentVolumeReclaimPolicy: Retain
  hostPath:
    path: /mnt/pv-data
```

### Verify

```bash
V() { kubectl get pv pv-data -o jsonpath="$1"; }
eq "$(V '{.spec.capacity.storage}')" "1Gi" "capacity"
eq "$(V '{.spec.accessModes[0]}')" "ReadWriteOnce" "accessMode"
eq "$(V '{.spec.storageClassName}')" "manual" "storageClassName"
eq "$(V '{.spec.hostPath.path}')" "/mnt/pv-data" "hostPath"
eq "$(V '{.spec.persistentVolumeReclaimPolicy}')" "Retain" "reclaimPolicy"
echo OK
```

## Task 2 - PersistentVolumeClaim (15%)

Create a PersistentVolumeClaim named `pvc-data` in namespace `storage` requesting `500Mi` with access mode `ReadWriteOnce` and `storageClassName: manual`. It must bind to `pv-data`.

### Solution

```yaml
apiVersion: v1
kind: PersistentVolumeClaim
metadata:
  name: pvc-data
  namespace: storage
spec:
  accessModes: [ReadWriteOnce]
  storageClassName: manual
  resources:
    requests:
      storage: 500Mi
```

### Verify

```bash
for i in $(seq 1 20); do
  [ "$(kubectl -n storage get pvc pvc-data -o jsonpath='{.status.phase}')" = Bound ] && break; sleep 2
done
eq "$(kubectl -n storage get pvc pvc-data -o jsonpath='{.status.phase}')" "Bound" "PVC phase"
eq "$(kubectl -n storage get pvc pvc-data -o jsonpath='{.spec.volumeName}')" "pv-data" "bound volume"
echo OK
```

## Task 3 - Use the claim in a Pod (20%)

Create a Pod named `writer` in namespace `storage`, image `busybox:1.36`, that mounts PVC `pvc-data` at `/data` and runs:

`sh -c "echo hello > /data/hello.txt && sleep 3600"`

### Solution

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: writer
  namespace: storage
spec:
  containers:
  - name: writer
    image: busybox:1.36
    command: ["sh", "-c", "echo hello > /data/hello.txt && sleep 3600"]
    volumeMounts:
    - name: data
      mountPath: /data
  volumes:
  - name: data
    persistentVolumeClaim:
      claimName: pvc-data
```

### Verify

```bash
kubectl -n storage wait --for=condition=Ready pod/writer --timeout=90s || fail "pod writer not ready"
eq "$(kubectl -n storage get pod writer -o jsonpath='{.spec.volumes[*].persistentVolumeClaim.claimName}')" "pvc-data" "claim used"
eq "$(kubectl -n storage exec writer -- cat /data/hello.txt)" "hello" "file content"
echo OK
```

## Task 4 - Default StorageClass and dynamic provisioning (30%)

- Write the name of the cluster's **default** StorageClass into `/tmp/cka/sc.txt`
- Create a PVC named `auto-pvc` in namespace `storage` requesting `100Mi` (ReadWriteOnce) **without** specifying a StorageClass
- Create a Pod `auto-pod` (image `busybox:1.36`, command `sleep 3600`) that mounts `auto-pvc` at `/mnt`. The PVC must end up `Bound` and the Pod `Running`.

### Solution

```bash
kubectl get sc                     # the one marked (default)
kubectl get sc -o jsonpath='{.items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")].metadata.name}' > /tmp/cka/sc.txt
```

Then create the PVC and the Pod with `volumes.persistentVolumeClaim.claimName: auto-pvc` and a `volumeMount` to `/mnt`.

### Verify

```bash
want=$(kubectl get sc -o jsonpath='{.items[?(@.metadata.annotations.storageclass\.kubernetes\.io/is-default-class=="true")].metadata.name}')
eq "$(tr -d '[:space:]' < /tmp/cka/sc.txt)" "$want" "sc.txt"
kubectl -n storage wait --for=condition=Ready pod/auto-pod --timeout=90s || fail "pod auto-pod not ready"
eq "$(kubectl -n storage get pvc auto-pvc -o jsonpath='{.status.phase}')" "Bound" "auto-pvc phase"
eq "$(kubectl -n storage get pvc auto-pvc -o jsonpath='{.spec.resources.requests.storage}')" "100Mi" "auto-pvc size"
eq "$(kubectl -n storage get pod auto-pod -o jsonpath='{.spec.volumes[*].persistentVolumeClaim.claimName}')" "auto-pvc" "claim used"
echo OK
```

## Task 5 - Shared emptyDir between containers (20%)

Create a Pod named `shared` in namespace `storage` with two `busybox:1.36` containers sharing an `emptyDir` volume named `logs`, mounted at `/var/log/app` in both:

- container `writer`: `sh -c "while true; do date >> /var/log/app/app.log; sleep 5; done"`
- container `reader`: `sh -c "tail -F /var/log/app/app.log"`

### Solution

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: shared
  namespace: storage
spec:
  volumes:
  - name: logs
    emptyDir: {}
  containers:
  - name: writer
    image: busybox:1.36
    command: ["sh", "-c", "while true; do date >> /var/log/app/app.log; sleep 5; done"]
    volumeMounts: [{name: logs, mountPath: /var/log/app}]
  - name: reader
    image: busybox:1.36
    command: ["sh", "-c", "tail -F /var/log/app/app.log"]
    volumeMounts: [{name: logs, mountPath: /var/log/app}]
```

### Verify

```bash
kubectl -n storage wait --for=condition=Ready pod/shared --timeout=90s || fail "pod shared not ready"
P() { kubectl -n storage get pod shared -o jsonpath="$1"; }
eq "$(P '{.spec.volumes[?(@.name=="logs")].emptyDir}')" "{}" "emptyDir volume named logs"
eq "$(P '{.spec.containers[*].name}')" "writer reader" "container names"
sleep 7
[ -n "$(kubectl -n storage logs shared -c reader | head -1)" ] || fail "reader container has no log output"
echo OK
```
