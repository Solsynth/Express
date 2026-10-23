SERVICE := distribution

.PHONY: run build build-cli build-express test check web web-dev

# Build the console into web/dist; the service serves it from [web] distPath.
web:
	cd web && bun install --frozen-lockfile && bun run build

web-dev:
	cd web && bun run dev

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
