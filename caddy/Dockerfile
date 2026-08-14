FROM caddy:2.11.2-builder AS builder

RUN xcaddy build \
    --with github.com/corazawaf/coraza-caddy/v2@v2.5.0 \
    --with github.com/greenpau/caddy-security \
    --with github.com/mholt/caddy-ratelimit \
    --with github.com/caddyserver/transform-encoder \
    --with github.com/caddyserver/replace-response

FROM caddy:2.11.2-alpine

COPY --from=builder /usr/bin/caddy /usr/bin/caddy

# R4-39d: distro-managed pins — apk resolves within the pinned alpine tag;
# ca-certificates carries an explicit version floor. See docs/SUPPLY_CHAIN_PINS.md.
RUN apk add --no-cache tzdata 'ca-certificates>=20240226' libcap

# R4-38: run as non-root. setcap grants the caddy binary permission to bind
# :80/:443; /data and /config (XDG dirs used by the stock image) are chowned.
RUN setcap 'cap_net_bind_service=+ep' /usr/bin/caddy && \
    adduser -D -u 10001 appuser && \
    chown -R appuser:appuser /data /config

USER appuser

CMD ["caddy", "run", "--config", "/etc/caddy/Caddyfile", "--adapter", "caddyfile"]
