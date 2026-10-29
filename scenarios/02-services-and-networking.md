---
title: Services & Networking
description: ClusterIP and NodePort services, NetworkPolicy, Ingress and cluster DNS.
difficulty: medium
domains: Services & Networking (20%)
duration: 40
pass: 66
---

# Services & Networking

Everything happens in namespace `net`. Save any requested files under `/tmp/cka`.

## Setup

```bash
kubectl create namespace net
kubectl -n net create deployment api --image=nginx:1.25 --replicas=2
kubectl -n net create deployment web --image=nginx:1.25
kubectl -n net expose deployment web --name=web-svc --port=80
kubectl -n net label deployment api tier=backend
kubectl -n net rollout status deployment/api --timeout=120s
kubectl -n net rollout status deployment/web --timeout=120s
```

## Cleanup

```bash
kubectl delete namespace net --ignore-not-found --timeout=180s
true
```

## Task 1 - ClusterIP Service (20%)

Expose the Deployment `api` with a Service named `api-svc` of type ClusterIP, listening on port `8080` and forwarding to container port `80`.

### Solution

```bash
kubectl -n net expose deployment api --name=api-svc --port=8080 --target-port=80
```

### Verify

```bash
S() { kubectl -n net get svc api-svc -o jsonpath="$1"; }
eq "$(S '{.spec.type}')" "ClusterIP" "type"
eq "$(S '{.spec.ports[0].port}')" "8080" "port"
eq "$(S '{.spec.ports[0].targetPort}')" "80" "targetPort"
[ -n "$(kubectl -n net get endpoints api-svc -o jsonpath='{.subsets[0].addresses[0].ip}')" ] || fail "service has no endpoints"
echo OK
```

## Task 2 - NodePort Service (15%)

Create a Service named `api-np` for the Deployment `api`, type NodePort, port `80`, with nodePort fixed to `30080`.

### Solution

```bash
kubectl -n net create service nodeport api-np --tcp=80:80 --node-port=30080 $do > np.yaml
# change selector from app: api-np to app: api
kubectl apply -f np.yaml
```

### Verify

```bash
S() { kubectl -n net get svc api-np -o jsonpath="$1"; }
eq "$(S '{.spec.type}')" "NodePort" "type"
eq "$(S '{.spec.ports[0].nodePort}')" "30080" "nodePort"
eq "$(S '{.spec.ports[0].port}')" "80" "port"
[ -n "$(kubectl -n net get endpoints api-np -o jsonpath='{.subsets[0].addresses[0].ip}')" ] || fail "service has no endpoints (check selector)"
echo OK
```

## Task 3 - NetworkPolicy (25%)

Create a NetworkPolicy named `db-allow-api` in namespace `net` that:

- applies to Pods labelled `app=db`
- allows **ingress** only from Pods labelled `app=api` (same namespace) on TCP port `5432`
- doesn't change egress behaviour

### Solution

```yaml
apiVersion: networking.k8s.io/v1
kind: NetworkPolicy
metadata:
  name: db-allow-api
  namespace: net
spec:
  podSelector:
    matchLabels:
      app: db
  policyTypes: [Ingress]
  ingress:
  - from:
    - podSelector:
        matchLabels:
          app: api
    ports:
    - protocol: TCP
      port: 5432
```

### Verify

```bash
N() { kubectl -n net get networkpolicy db-allow-api -o jsonpath="$1"; }
eq "$(N '{.spec.podSelector.matchLabels.app}')" "db" "podSelector"
contains "$(N '{.spec.policyTypes}')" "Ingress" "policyTypes"
eq "$(N '{.spec.ingress[0].from[0].podSelector.matchLabels.app}')" "api" "from podSelector"
eq "$(N '{.spec.ingress[0].ports[0].port}')" "5432" "port"
eq "$(N '{.spec.ingress[0].ports[0].protocol}')" "TCP" "protocol"
eq "$(N '{.spec.ingress[*].from[*].podSelector.matchLabels.app}')" "api" "only one allowed source"
echo OK
```

## Task 4 - Ingress (20%)

Create an Ingress named `web-ing` in namespace `net` routing host `shop.example.com`, path `/` (type `Prefix`) to the existing Service `web-svc` on port `80`.

### Solution

```bash
kubectl -n net create ingress web-ing --rule="shop.example.com/*=web-svc:80"
```

### Verify

```bash
I() { kubectl -n net get ingress web-ing -o jsonpath="$1"; }
eq "$(I '{.spec.rules[0].host}')" "shop.example.com" "host"
eq "$(I '{.spec.rules[0].http.paths[0].path}')" "/" "path"
eq "$(I '{.spec.rules[0].http.paths[0].pathType}')" "Prefix" "pathType"
eq "$(I '{.spec.rules[0].http.paths[0].backend.service.name}')" "web-svc" "backend service"
eq "$(I '{.spec.rules[0].http.paths[0].backend.service.port.number}')" "80" "backend port"
echo OK
```

## Task 5 - Cluster DNS (20%)

- Write the ClusterIP of the cluster DNS service (`kube-dns` in `kube-system`) into `/tmp/cka/dns-ip.txt`
- Write the fully qualified domain name of the Service `web-svc` in namespace `net` (using the `cluster.local` domain) into `/tmp/cka/fqdn.txt`

### Solution

```bash
kubectl -n kube-system get svc kube-dns -o jsonpath='{.spec.clusterIP}' > /tmp/cka/dns-ip.txt
echo web-svc.net.svc.cluster.local > /tmp/cka/fqdn.txt
```

### Verify

```bash
want=$(kubectl -n kube-system get svc kube-dns -o jsonpath='{.spec.clusterIP}')
eq "$(tr -d '[:space:]' < /tmp/cka/dns-ip.txt)" "$want" "dns-ip.txt"
eq "$(tr -d '[:space:]' < /tmp/cka/fqdn.txt)" "web-svc.net.svc.cluster.local" "fqdn.txt"
echo OK
```
