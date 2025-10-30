# syntax=docker/dockerfile:1

FROM golang:1.21-alpine AS builder
WORKDIR /src
COPY go.mod ./
# go.sum is intentionally absent because the module currently has no external dependencies.
# Running go mod download keeps this layer stable even when new dependencies are added later on.
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o /bin/ratelimiter ./cmd/ratelimiter

FROM alpine:3.19
WORKDIR /app
COPY --from=builder /bin/ratelimiter /usr/local/bin/ratelimiter
EXPOSE 8080 8081
ENV HTTP_ADDR=:8080 \
    GRPC_ADDR=:8081 \
    NATS_URL=
ENTRYPOINT ["/usr/local/bin/ratelimiter"]
