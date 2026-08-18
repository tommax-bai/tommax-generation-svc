.PHONY: build run run-api run-worker lint test e2e
build:
	go build -o bin/generation ./cmd/server
run: build
	./bin/generation -config configs/config.yaml
run-api: build
	./bin/generation -config configs/config.yaml -mode api
run-worker: build
	./bin/generation -config configs/config.yaml -mode worker
lint:
	golangci-lint run ./...
test:
	go test -race ./...
e2e:
	bash scripts/e2e.sh
