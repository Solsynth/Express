# syntax=docker/dockerfile:1

# ---- SPA build (web/) ----
FROM oven/bun:1.4-alpine AS web
WORKDIR /web
# @solsynth/sunkenland lives on GitHub Packages, which authenticates every
# install — public packages included. The token is a build arg, so it never
# lands in a layer.
COPY web/package.json web/bun.lock web/.npmrc ./
ARG NODE_AUTH_TOKEN
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
# Work from /app so the loader's default config path and the relative [web]
# distPath both resolve inside the image: it reads ./config.example.toml and
# "web/dist" against this directory, so the console lands at /app/web/dist.
WORKDIR /app
COPY --from=build /out/distribution /app/distribution
COPY --from=web /web/dist /app/web/dist
COPY config.example.toml /app/config.example.toml

USER nonroot:nonroot
EXPOSE 8080 9090
ENTRYPOINT ["/app/distribution"]

