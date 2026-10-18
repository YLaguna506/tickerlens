# syntax=docker/dockerfile:1
# Build stage: compile a static binary.
FROM golang:1.27-alpine AS build
WORKDIR /src

# Download modules first so this layer is cached when only code changes.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

# The build cache survives between builds, so a rebuild after a code change
# (`docker compose watch`) only recompiles the packages that changed.
COPY . .
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build     CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/api ./cmd/api

# Run stage: a minimal image with no shell, running as a non-root user.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/api /api
EXPOSE 8080
ENTRYPOINT ["/api"]
