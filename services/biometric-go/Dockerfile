FROM golang:1.22 AS builder

WORKDIR /app
COPY go.mod go.sum ./
RUN go mod download

COPY *.go .
RUN CGO_ENABLED=0 go build -o /abis-pipeline .

FROM gcr.io/distroless/static-debian12

COPY --from=builder /abis-pipeline /abis-pipeline

EXPOSE 8092

ENTRYPOINT ["/abis-pipeline"]
