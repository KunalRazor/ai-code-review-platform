# Deploying the platform

The platform is five services (receiver, worker, api, analyzer, dashboard) plus
Postgres + Redis. For local runs use **docker-compose** (see the top-level
README). This doc covers **Kubernetes**.

The three Go binaries share one image (`acr-go`); the analyzer and dashboard
have their own images.

## 1. Build and push images

Your Mac is arm64; most clusters are amd64, so build for the cluster arch.

```bash
echo "$GHCR_TOKEN" | docker login ghcr.io -u OWNER --password-stdin

docker buildx build --platform linux/amd64 -t ghcr.io/OWNER/acr-go:latest --push .
docker buildx build --platform linux/amd64 -t ghcr.io/OWNER/acr-analyzer:latest --push ./analyzer
docker buildx build --platform linux/amd64 -t ghcr.io/OWNER/acr-dashboard:latest --push ./web/dashboard
```

Replace `OWNER` in the `deploy/k8s/*.yaml` image fields with your namespace.

## 2. Create the secret (only if OFFLINE=false / real providers)

```bash
kubectl create secret generic acr-secrets \
  --from-literal=GITHUB_WEBHOOK_SECRET='your-webhook-secret' \
  --from-literal=GITHUB_TOKEN='github_pat_xxx' \
  --from-literal=ANTHROPIC_API_KEY='sk-ant-xxx'
```

## 3. Apply everything

Manifests are numbered so `kubectl apply -f .` applies them in order:

```bash
kubectl apply -f deploy/k8s/
```

This creates: ConfigMap, Secret, Postgres (+PVC), Redis, analyzer, receiver,
worker (+HPA), api, dashboard, and the Ingress.

## 4. Wire up the webhook

```bash
kubectl get ingress ai-code-review
```

Point DNS at the ingress load balancer, then in GitHub → repo Settings →
Webhooks: Payload URL `https://YOUR_DOMAIN/webhook`, the same
`GITHUB_WEBHOOK_SECRET`, event = Pull requests.

### Cluster prerequisites
- nginx **ingress controller** (public LoadBalancer)
- **cert-manager** with a `letsencrypt-prod` ClusterIssuer (TLS)
- **metrics-server** (for the worker HPA)

## Keyless demo on a local cluster (kind)

```bash
kind create cluster
for img in acr-go acr-analyzer acr-dashboard; do :; done
docker build -t acr-go:dev .
docker build -t acr-analyzer:dev ./analyzer
docker build -t acr-dashboard:dev ./web/dashboard
kind load docker-image acr-go:dev acr-analyzer:dev acr-dashboard:dev
# edit deploy/k8s images to the :dev tags, set imagePullPolicy: IfNotPresent,
# and set OFFLINE: "true" in 00-config.yaml, then:
kubectl apply -f deploy/k8s/
kubectl port-forward svc/dashboard 5173:80   # open http://localhost:5173
```
