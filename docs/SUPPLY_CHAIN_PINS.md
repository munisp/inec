# SUPPLY_CHAIN_PINS.md — image pinning policy (R4-39d)

## Policy

1. **Every image reference must carry a fixed tag.** `scripts/check_image_pins.sh`
   (wired into CI) fails the build on any untagged or `:latest` reference in
   `docker-compose*.yml` / `Dockerfile*`.
2. **Digest pinning (`@sha256:`) is the gold standard** and is required for
   internet-pulled runtime images on the production path before go-live.
   Record the multi-arch digest at pin time (e.g. permify v1.7.2 =
   `sha256:99edbbcf79faa5243bb6d4f6d72b1e34548143df08a089ba3c995c83ffa0a8b4`,
   see the compose comment). Digests are intentionally NOT yet applied
   fleet-wide because they must be re-resolved on every base-image bump;
   tags are the enforced minimum today, digests are the rollout target.
3. **Floating minor tags** (`python:3.12-slim`, `node:22-alpine`,
   `golang:1.26-alpine`, `rust:1-bookworm`, `postgres:16-alpine`,
   `redis:7-alpine`) remain accepted for builder/base images; they receive
   upstream patch fixes automatically. Recorded as accepted risk — tighten to
   patch-level pins or digests in the next hardening round.
4. **Toolchain skew** (go 1.22 vs 1.26, node 20/22/26, rust 1.88/1.89/1-bookworm,
   debian bookworm/trixie) is recorded; per-module `go.mod` matches its
   Dockerfile. Not changed in R4 (out of minimal-fix scope).

## apt/apk package pinning

Distro package versions are managed by the base image's distribution (Debian
trixie/bookworm, Alpine 3.x): exact `pkg=version` pins break whenever the
distro archive rolls forward. Policy:

- Security-sensitive packages (`openssl`, `ca-certificates`, `libssl3`,
  `libssl-dev`) are pinned to the distro-tracked version pattern where the
  Dockerfile installs them, with a comment; rebuilds pick up distro security
  updates.
- Remaining unpinned `apt-get install` / `apk add` package sets (build tools:
  `pkg-config`, `g++`, `curl`, etc.) are distro-managed by design — recorded
  here as accepted risk rather than pinned to versions that will 404.
