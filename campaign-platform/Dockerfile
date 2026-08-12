# INEC Campaign Platform — production image
#
# Required runtime environment (boot fails fast without these in production):
#   JWT_SECRET                 >= 32 random chars; signs session JWTs
#   POSTGRES_URL|DATABASE_URL  PostgreSQL connection string
# Optional:
#   PORT                       listen port (pinned to 8206 below)
#   VITE_APP_ID, OWNER_OPEN_ID, BUILT_IN_FORGE_API_URL, BUILT_IN_FORGE_API_KEY
#   COOKIE_SAMESITE, COOKIE_SECURE, AUTH_RETURN_SESSION_COOKIE
#
# Schema changes are applied out-of-band with `pnpm db:migrate`
# (drizzle-kit migrate) against the journaled files in drizzle/ — migration
# SQL is NEVER generated at deploy time. See README.md (migration policy).

FROM node:22-alpine AS builder
RUN corepack enable
WORKDIR /app
COPY package.json pnpm-lock.yaml ./
COPY patches ./patches
RUN pnpm install --frozen-lockfile
COPY . .
# Rollup's native binary segfaults under this host's QEMU x86_64 emulation;
# force the pure-JS fallback.
ENV ROLLUP_NO_NATIVE=1
RUN pnpm build

FROM node:22-alpine
RUN corepack enable
WORKDIR /app
ENV NODE_ENV=production
# The server reads $PORT (default 3000) — pin it to the exposed port so the
# container actually serves on what we EXPOSE/advertise.
ENV PORT=8206
COPY package.json pnpm-lock.yaml ./
COPY patches ./patches
# Production dependencies only — devDependencies (vite, tsx, typescript, ...)
# must not ship in the runtime image.
RUN pnpm install --frozen-lockfile --prod
COPY --from=builder /app/dist ./dist
COPY --from=builder /app/drizzle ./drizzle
# Run as the unprivileged node user, not root.
USER node
EXPOSE 8206
HEALTHCHECK --interval=30s --timeout=3s --start-period=10s --retries=3 \
  CMD wget -qO- http://localhost:${PORT:-8206}/api/v1/campaign/health || exit 1
CMD ["node", "dist/index.js"]
