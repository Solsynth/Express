SERVICE := distribution

.PHONY: run build build-cli build-express test check

build-cli build-express:
	mkdir -p bin
	go build -o bin/express ./cmd/express

run:
	CONFIG_PATH=config.example.toml go run ./cmd/distribution

build:
	go build ./...

test:
	go test ./...

check:
	gofmt -w cmd internal
	go vet ./...
	go test ./...
