.PHONY: build run test docker-build compose-up compose-down compose-logs kube-apply kube-delete kube-smoke kind-up kind-down

BINARY := ratelimiter
IMAGE ?= rate-limiter:local
KUBE_NAMESPACE ?= rate-limiter
DOCKER_COMPOSE ?= $(shell if command -v docker-compose >/dev/null 2>&1; then echo docker-compose; else echo "docker compose"; fi)
COMPOSE_PROJECT ?= deploy

build:
	mkdir -p bin
	GOOS=linux GOARCH=amd64 CGO_ENABLED=0 go build -o bin/$(BINARY) ./cmd/ratelimiter

run:
	go run ./cmd/ratelimiter

test:
	go test -v ./...

docker-build:
	docker build -t $(IMAGE) .

compose-up:
DOCKER_COMPOSE="$(DOCKER_COMPOSE)" \
COMPOSE_PROJECT_DIR=deploy \
./hack/compose-up.sh
COMPOSE_PROJECT=$(COMPOSE_PROJECT) \
COMPOSE_PROJECT_DIR=deploy \
DOCKER_COMPOSE="$(DOCKER_COMPOSE)" \
./hack/wait-compose.sh

compose-down:
	cd deploy && $(DOCKER_COMPOSE) down

compose-logs:
	cd deploy && $(DOCKER_COMPOSE) logs -f

kube-apply:
	kubectl apply -f deploy/kubernetes/nats.yaml
	kubectl apply -f deploy/kubernetes/rate-limiter.yaml

kube-delete:
	kubectl delete -f deploy/kubernetes/rate-limiter.yaml || true
	kubectl delete -f deploy/kubernetes/nats.yaml || true

kube-smoke:
	IMAGE=$(IMAGE) NAMESPACE=$(KUBE_NAMESPACE) hack/ci/k8s-smoke.sh

kind-up:
	hack/kind-up.sh

kind-down:
	kind delete cluster --name rate-limiter || true
