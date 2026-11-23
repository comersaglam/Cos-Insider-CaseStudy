# syntax=docker/dockerfile:1
# One image, both binaries (ADR 0004, 0019). Build: docker build -t notifications .

# The build stage has the Go toolchain; nothing of it reaches the final image. The tag matches go.mod.
FROM golang:1.27.4-alpine AS build
WORKDIR /src

# Dependencies first, so editing code does not download them again.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
# Static binaries (no cgo), without local paths (-trimpath) or debug symbols (-s -w).
RUN --mount=type=cache,target=/go/pkg/mod --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/ ./cmd/...

# The final stage holds the binaries, CA certificates and a non-root user, and no shell or package manager.
# distroless has no version tags, so it is pinned by digest; a dependency bot keeps the digest current.
FROM gcr.io/distroless/static-debian13:nonroot@sha256:8ce6dd87e9823c5b48dcfb2c8604714ccb351ce59607327bcc4957448da279b5
COPY --from=build /out/api /out/worker /usr/local/bin/
USER nonroot:nonroot
EXPOSE 8080
CMD ["api"]
