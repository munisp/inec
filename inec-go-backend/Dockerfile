FROM golang:1.26-alpine AS builder
RUN apk add --no-cache gcc musl-dev sqlite-dev
WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download
COPY *.go ./
COPY migrations/ ./migrations/
RUN CGO_ENABLED=1 GOOS=linux go build -o inec-go-backend .

FROM alpine:3.21
RUN apk add --no-cache sqlite-libs ca-certificates
WORKDIR /app
COPY --from=builder /app/inec-go-backend .
COPY --from=builder /app/migrations ./migrations/
EXPOSE 8088
ENV PORT=8088
CMD ["./inec-go-backend"]
