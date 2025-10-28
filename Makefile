.PHONY: build run test docker-build compose-up compose-down kube-apply kube-delete

BINARY := ratelimiter
IMAGE ?= rate-limiter:local

build:
	mkdir -p bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/$(BINARY) ./cmd/ratelimiter

run:
	go run ./cmd/ratelimiter

test:
	go test ./...

docker-build:
	docker build -t $(IMAGE) .

compose-up:
	cd deploy && docker compose up --build

compose-down:
	cd deploy && docker compose down

kube-apply:
	kubectl apply -f deploy/kubernetes/nats.yaml
	kubectl apply -f deploy/kubernetes/rate-limiter.yaml

kube-delete:
	kubectl delete -f deploy/kubernetes/rate-limiter.yaml || true
	kubectl delete -f deploy/kubernetes/nats.yaml || true
