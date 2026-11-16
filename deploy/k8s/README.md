# DarkStream on Kubernetes

Manifests to run the DarkStream master and workers on Kubernetes.

```
deploy/k8s/
├── 00-namespace.yaml          # darkstream namespace
├── 10-secret.yaml             # master API key + per-worker tokens (EDIT FIRST)
├── 20-configmap-master.yaml   # master config.yaml (scan locations, DB, conversion)
├── 21-configmap-worker.yaml   # worker config template (API key injected from Secret)
├── 30-master-deployment.yaml  # master Deployment + PVC + Service (single replica!)
├── 40-worker-daemonset.yaml   # one worker per node, hostPath media mount
└── 50-servicemonitor.yaml     # Prometheus Operator scrape (optional)
```

## Prerequisites

- A cluster with a default StorageClass (for the master's 5 GiB PVC).
- Media available on each node at `/srv/media` (adjust the `hostPath` in
  `30-master-deployment.yaml` and `40-worker-daemonset.yaml` to your layout).
- Workers need ffmpeg and (optionally) a GPU; the `worker` image target already
  ships ffmpeg and the Vulkan runtime. For NVIDIA GPUs add the
  `nvidia.com/gpu` resource limit per node.
- Prometheus Operator (only for `50-servicemonitor.yaml`).

## Install

```bash
# 1. Build and load the images (or push to your registry and edit the manifests)
docker build -t darkstream:latest --target master .
docker build -t darkstream:latest --target worker .
kind load docker-image darkstream:latest   # or: docker push + imagePullPolicy

# 2. Generate secrets and edit deploy/k8s/10-secret.yaml
MASTER_KEY=$(openssl rand -hex 32)
WORKER1_TOKEN=$(openssl rand -hex 32)
# put $MASTER_KEY as master-api-key, $WORKER1_TOKEN as worker-token-1
# and set the same worker token under server.worker_tokens in
# deploy/k8s/20-configmap-master.yaml

# 3. Adjust paths/locations in the two ConfigMaps, then apply in order
kubectl apply -f deploy/k8s/00-namespace.yaml
kubectl apply -f deploy/k8s/10-secret.yaml   # includes the darkstream-tls Secret (cert/key)
kubectl apply -f deploy/k8s/20-configmap-master.yaml
kubectl apply -f deploy/k8s/21-configmap-worker.yaml
kubectl apply -f deploy/k8s/30-master-deployment.yaml
kubectl apply -f deploy/k8s/40-worker-daemonset.yaml
kubectl apply -f deploy/k8s/50-servicemonitor.yaml   # optional
```

## Notes

- The master Deployment is pinned to **1 replica** (`strategy: Recreate`):
  the job database is SQLite and single-writer. Scale conversion throughput by
  adding workers, not masters.
- `/healthz` (liveness) and `/healthz/ready` (readiness) are wired into the
  probes. Readiness requires the database to answer **and** at least one scan
  location to be running, so a master with zero active locations is removed
  from the Service endpoints.
- Metrics are exposed on the same port at `/metrics`
  (`darkstream_jobs_*`, `darkstream_workers_*`, `darkstream_transfer_*`,
  `darkstream_api_*`, `darkstream_scanner_*`). Import
  `monitoring/grafana/darkstream-overview.json` into Grafana with a
  Prometheus datasource.
- The master auto-generates `config.yaml` when the mounted file is absent, but
  mounting the ConfigMap is recommended so scan locations persist and are
  reviewable in git.
- API key auth: set `DARKSTREAM_API_KEY` (master) and `worker.api_key`
  (workers). Per-worker tokens (`server.worker_tokens`) give each worker its
  own credential on `/api/worker/*` and appear as `worker:<id>` in the audit
  log (`GET /api/audit-log`). SSE streams in the Web UI use short-lived signed
  tokens (`POST /api/stream-token`) so the master key never appears in URLs.

- The worker entrypoint renders its config with `sed "s|...|...|g"` — if an
  API key or token contains `|`, `&`, or `\`, switch the delimiter or use a
  different rendering mechanism.
- The master requires TLS when an API key is set, so `darkstream-tls` is
  mandatory in the manifests as shipped; the Service exposes 8443 (https).
