FROM golang:1.22 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY *.go .
RUN CGO_ENABLED=0 go build -o /abis-pipeline .

FROM alpine:3.21 AS healthcheck-tools
# Static busybox supplies wget for the compose healthcheck; the distroless
# runtime image contains no shell, curl, or wget of its own.
# R4-39d: distro-managed pins — apk resolves within the pinned alpine tag;
# ca-certificates carries an explicit version floor. See docs/SUPPLY_CHAIN_PINS.md.
RUN apk add --no-cache busybox-static

FROM gcr.io/distroless/static-debian12:nonroot  # runs as uid 65532

# R4-38: explicit non-root user (distroless nonroot, uid 65532).
USER 65532:65532

COPY --from=builder /abis-pipeline /abis-pipeline
COPY --from=healthcheck-tools /bin/busybox.static /usr/bin/busybox

EXPOSE 8092

ENTRYPOINT ["/abis-pipeline"]
