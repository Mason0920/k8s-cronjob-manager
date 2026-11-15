BINARY  := bin/cronjob-manager
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
IMAGE   ?= k8s-cronjob-manager:dev
KIND_CLUSTER ?= kind

.PHONY: all ui build test vet lint run docker kind-load deploy example clean

all: build

## ui: build the React UI into web/dist/ui (needs Node 24+)
ui:
	cd web && npm ci --no-audit --no-fund && npm run build

## build: UI + Go binary with the UI embedded
build: ui
	CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$(VERSION)" -o $(BINARY) ./cmd/cronjob-manager

## test: Go unit tests with the race detector
test:
	go test -race ./...

vet:
	go vet ./...
	@test -z "$$(gofmt -l .)" || (echo "gofmt needed:"; gofmt -l .; exit 1)

## run: run against the current kubeconfig context (NAMESPACES required)
run:
	NAMESPACES=$${NAMESPACES:-batch-jobs} go run ./cmd/cronjob-manager

docker:
	docker build --build-arg VERSION=$(VERSION) -t $(IMAGE) .

## kind-load: load the image into a local kind cluster
kind-load: docker
	kind load docker-image $(IMAGE) --name $(KIND_CLUSTER)

## example: sample namespace and CronJobs
example:
	kubectl apply -f deploy/example/batch-jobs.yaml

## deploy: RBAC, Deployment and Service
deploy:
	kubectl apply -k deploy/

clean:
	rm -rf bin web/dist/ui
