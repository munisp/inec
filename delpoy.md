# NGApp

## Building the images

### Prerequisites

- Docker installed and authenticated against the registry used by the demo deployment:
  `registry.digitalocean.com/talentgraph-auth` (e.g. via `doctl registry login`).

### Tag convention

Image tags are immutable and hand-assigned — there is no CI job or script that generates
them for this registry. The convention used so far is:

```
<label>-<UTC date>-<UTC time>
```

e.g. `archive-20260612-205000`. `label` is a free-form description of the change batch
(`demo`, `archive`, etc.); the timestamp can be generated with `date -u +%Y%m%d-%H%M%S`.

This is unrelated to the frontend's service-worker cache version, which is stamped
automatically on every `npm run build` by the Vite plugin in `inec-frontend/vite.config.ts`.

### Backend

```bash
docker build -t registry.digitalocean.com/talentgraph-auth/inec-backend:<tag> inec-go-backend
docker push registry.digitalocean.com/talentgraph-auth/inec-backend:<tag>
```

### Frontend

```bash
docker build -t registry.digitalocean.com/talentgraph-auth/inec-frontend:<tag> inec-frontend
docker push registry.digitalocean.com/talentgraph-auth/inec-frontend:<tag>
```

### Deploying a new build

After pushing, update the corresponding `image:` tag in `k8s/inec-demo/app.yaml` and apply:

```bash
kubectl apply --dry-run=server -f k8s/inec-demo/app.yaml
kubectl apply -f k8s/inec-demo/app.yaml
kubectl rollout status deployment/inec-backend -n inec
kubectl rollout status deployment/inec-frontend -n inec
```