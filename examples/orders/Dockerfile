# ---- Stage 1: build ---------------------------------------------------------
# Pin images by digest in production (image@sha256:...) so builds are
# reproducible and a re-tagged image cannot change what you ship.
FROM golang:1.27 AS build
WORKDIR /src

# Download modules in their own layer: it is cached until go.mod/go.sum
# change, so code-only changes rebuild in seconds.
COPY go.mod ./
RUN go mod download

COPY . .
ARG VERSION=dev
# CGO_ENABLED=0: a static binary with no libc dependency, so it runs on a
# minimal base image. -trimpath removes local paths from the binary.
RUN CGO_ENABLED=0 go build -trimpath \
      -ldflags="-s -w -X github.com/acme/orders/internal/buildinfo.version=${VERSION}" \
      -o /out/ordersd ./cmd/ordersd

# ---- Stage 2: run -----------------------------------------------------------
# distroless/static: no shell, no package manager, only CA certificates,
# timezone data and /etc/passwd. Fewer files = fewer vulnerabilities.
# The :nonroot tag runs as UID 65532, never as root.
FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/ordersd /ordersd
USER nonroot:nonroot
EXPOSE 8080
ENTRYPOINT ["/ordersd"]
