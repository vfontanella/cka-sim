---
title: RBAC & Security
description: Roles, bindings, service accounts, security contexts, Secrets and auditing privileged Pods.
difficulty: medium
domains: Cluster Architecture, Installation & Configuration (25%)
duration: 45
pass: 66
---

# RBAC & Security

Save any requested files under `/tmp/cka`.

## Setup

```bash
kubectl create namespace sec
kubectl create namespace audit
cat <<YAML | kubectl apply -f -
apiVersion: v1
kind: Pod
metadata: {name: priv-a, namespace: audit}
spec:
  containers:
  - {name: c, image: busybox:1.36, command: [sleep, "3600"], securityContext: {privileged: true}}
---
apiVersion: v1
kind: Pod
metadata: {name: ok-b, namespace: audit}
spec:
  containers:
  - {name: c, image: busybox:1.36, command: [sleep, "3600"]}
---
apiVersion: v1
kind: Pod
metadata: {name: priv-c, namespace: audit}
spec:
  containers:
  - {name: c, image: busybox:1.36, command: [sleep, "3600"], securityContext: {privileged: true}}
YAML
kubectl -n audit wait --for=condition=Ready pod --all --timeout=120s
```

## Cleanup

```bash
kubectl delete namespace sec audit --ignore-not-found --timeout=180s
kubectl delete clusterrole node-reader --ignore-not-found
kubectl delete clusterrolebinding node-reader-binding --ignore-not-found
true
```

## Task 1 - Namespaced RBAC for a ServiceAccount (25%)

In namespace `sec`:

- create ServiceAccount `deployer`
- create Role `deploy-manager` allowing `get`, `list`, `create`, `update`, `delete` on `deployments` (API group `apps`)
- bind it to the ServiceAccount with RoleBinding `deployer-binding`

### Solution

```bash
kubectl -n sec create serviceaccount deployer
kubectl -n sec create role deploy-manager --verb=get,list,create,update,delete --resource=deployments.apps
kubectl -n sec create rolebinding deployer-binding --role=deploy-manager --serviceaccount=sec:deployer
```

### Verify

```bash
SA=system:serviceaccount:sec:deployer
eq "$(kubectl auth can-i create deployments.apps -n sec --as=$SA)" "yes" "create deployments"
eq "$(kubectl auth can-i delete deployments.apps -n sec --as=$SA)" "yes" "delete deployments"
eq "$(kubectl auth can-i list deployments.apps -n sec --as=$SA)" "yes" "list deployments"
eq "$(kubectl auth can-i delete pods -n sec --as=$SA)" "no" "delete pods must be denied"
eq "$(kubectl auth can-i create deployments.apps -n default --as=$SA)" "no" "other namespaces must be denied"
echo OK
```

## Task 2 - ClusterRole for a user (20%)

Create a ClusterRole `node-reader` that can `get`, `list` and `watch` `nodes`, and a ClusterRoleBinding `node-reader-binding` granting it to the user `jane`.

### Solution

```bash
kubectl create clusterrole node-reader --verb=get,list,watch --resource=nodes
kubectl create clusterrolebinding node-reader-binding --clusterrole=node-reader --user=jane
```

### Verify

```bash
eq "$(kubectl auth can-i list nodes --as=jane)" "yes" "list nodes"
eq "$(kubectl auth can-i watch nodes --as=jane)" "yes" "watch nodes"
eq "$(kubectl auth can-i delete nodes --as=jane)" "no" "delete nodes must be denied"
eq "$(kubectl auth can-i list pods -n sec --as=jane)" "no" "pods must be denied"
echo OK
```

## Task 3 - Hardened Pod (20%)

Create a Pod `secure-pod` in namespace `sec`, image `busybox:1.36`, command `sleep 3600`, with:

- runs as user `1000` and `runAsNonRoot: true`
- `allowPrivilegeEscalation: false`
- `readOnlyRootFilesystem: true`
- all Linux capabilities dropped

### Solution

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: secure-pod
  namespace: sec
spec:
  containers:
  - name: secure-pod
    image: busybox:1.36
    command: ["sleep", "3600"]
    securityContext:
      runAsUser: 1000
      runAsNonRoot: true
      allowPrivilegeEscalation: false
      readOnlyRootFilesystem: true
      capabilities:
        drop: ["ALL"]
```

### Verify

```bash
C() { kubectl -n sec get pod secure-pod -o jsonpath="{.spec.containers[0].securityContext$1}"; }
P() { kubectl -n sec get pod secure-pod -o jsonpath="{.spec.securityContext$1}"; }
uid="$(C '.runAsUser')$(P '.runAsUser')"
contains "$uid" "1000" "runAsUser"
contains "$(C '.runAsNonRoot')$(P '.runAsNonRoot')" "true" "runAsNonRoot"
eq "$(C '.allowPrivilegeEscalation')" "false" "allowPrivilegeEscalation"
eq "$(C '.readOnlyRootFilesystem')" "true" "readOnlyRootFilesystem"
eq "$(C '.capabilities.drop[0]')" "ALL" "capabilities.drop"
kubectl -n sec wait --for=condition=Ready pod/secure-pod --timeout=90s || fail "pod not ready"
echo OK
```

## Task 4 - Secrets in a Pod (20%)

In namespace `sec`:

- create a Secret `db-creds` with `username=admin` and `password=S3cret!`
- create a Pod `db-client` (image `busybox:1.36`, command `sleep 3600`) that
  - mounts the Secret as a volume at `/etc/db`
  - sets env var `DB_USER` from the key `username` of the Secret

### Solution

```bash
kubectl -n sec create secret generic db-creds --from-literal=username=admin --from-literal='password=S3cret!'
```

```yaml
apiVersion: v1
kind: Pod
metadata:
  name: db-client
  namespace: sec
spec:
  containers:
  - name: db-client
    image: busybox:1.36
    command: ["sleep", "3600"]
    env:
    - name: DB_USER
      valueFrom:
        secretKeyRef: {name: db-creds, key: username}
    volumeMounts:
    - {name: creds, mountPath: /etc/db}
  volumes:
  - name: creds
    secret:
      secretName: db-creds
```

### Verify

```bash
kubectl -n sec wait --for=condition=Ready pod/db-client --timeout=90s || fail "pod not ready"
eq "$(kubectl -n sec exec db-client -- cat /etc/db/password)" 'S3cret!' "password file"
eq "$(kubectl -n sec exec db-client -- printenv DB_USER)" "admin" "DB_USER"
eq "$(kubectl -n sec get pod db-client -o jsonpath='{.spec.containers[0].env[?(@.name=="DB_USER")].valueFrom.secretKeyRef.name}')" "db-creds" "env from secretKeyRef"
echo OK
```

## Task 5 - Find and remove privileged Pods (15%)

Some Pods in namespace `audit` run privileged containers. Write the names of **all** privileged Pods (one per line) to `/tmp/cka/privileged.txt` and then delete them. Don't touch the other Pods.

### Solution

```bash
kubectl -n audit get pods -o jsonpath='{range .items[?(@.spec.containers[*].securityContext.privileged==true)]}{.metadata.name}{"\n"}{end}' | tee /tmp/cka/privileged.txt
kubectl -n audit delete pod priv-a priv-c $now
```

### Verify

```bash
f=$(sort /tmp/cka/privileged.txt | tr -d ' ' | grep -v '^$' | tr '\n' ',')
eq "$f" "priv-a,priv-c," "privileged.txt content"
kubectl -n audit get pod ok-b >/dev/null 2>&1 || fail "pod ok-b must still exist"
for p in priv-a priv-c; do
  s=$(kubectl -n audit get pod $p -o jsonpath='{.metadata.deletionTimestamp}{.metadata.name}' 2>/dev/null)
  case "$s" in ""|*[0-9]*Z*) ;; *) fail "pod $p was not deleted";; esac
done
echo OK
```
