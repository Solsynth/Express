# syntax=docker/dockerfile:1

# ---- SPA build (web/) ----
FROM oven/bun:1.4-alpine AS web
WORKDIR /web
COPY web/package.json web/bun.lock ./
RUN bun install --frozen-lockfile
COPY web/ ./
# Skip vue-tsc here: the build stage must not depend on a host-only TypeScript
# bridge, and the CI Go image has no Node toolchain to run it.
RUN bun run build-only

# ---- Go build ----
FROM golang:1.26 AS build

WORKDIR /src
COPY go.mod go.sum ./
RUN go mod download
COPY . .

ARG VERSION=dev
ARG GIT_COMMIT=unknown
RUN CGO_ENABLED=0 go build -trimpath \
	-ldflags "-s -w -X main.version=${VERSION} -X main.gitCommit=${GIT_COMMIT}" \
	-o /out/distribution ./cmd/distribution

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/distribution /distribution
COPY --from=web /web/dist /app/web/dist
COPY config.example.toml /config.example.toml

USER nonroot:nonroot
EXPOSE 8080 9090
ENTRYPOINT ["/distribution"]
