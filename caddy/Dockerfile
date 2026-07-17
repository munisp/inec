FROM caddy:2.7.6-builder AS builder

RUN xcaddy build \
    --with github.com/corazawaf/coraza-caddy/v2 \
    --with github.com/greenpau/caddy-security \
    --with github.com/mholt/caddy-ratelimit \
    --with github.com/caddyserver/transform-encoder \
    --with github.com/caddyserver/replace-response

FROM caddy:2.7.6-alpine

COPY --from=builder /usr/bin/caddy /usr/bin/caddy

RUN apk add --no-cache tzdata ca-certificates

CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
