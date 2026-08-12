FROM golang:1.26-alpine AS builder
RUN apk add --no-cache gcc musl-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY internal/ ./internal/
COPY migrations/ ./migrations/
RUN CGO_ENABLED=1 GOOS=linux go build -o inec-go-backend .

FROM alpine:3.24
RUN apk add --no-cache ca-certificates postgresql16-client curl
WORKDIR /app
COPY --from=builder /app/inec-go-backend .
COPY --from=builder /app/migrations ./migrations/
# Run as a non-root user; /data is a named volume the app writes to.
RUN adduser -D -u 10001 app && mkdir -p /data && chown app:app /data
USER app
EXPOSE 8088
ENV PORT=8088
HEALTHCHECK --interval=30s --timeout=3s --start-period=5s --retries=3 \
  CMD curl -f http://localhost:8088/healthz || exit 1
CMD ["./inec-go-backend"]
