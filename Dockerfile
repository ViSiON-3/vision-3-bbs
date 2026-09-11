# docker run -d -p 2222:2222 -p 2323:2323 \
#   -v "$(pwd)/configs:/vision3/configs" \
#   -v "$(pwd)/data:/vision3/data" \
#   vision3

# ---------------------------------------------------------------------------
# Stage 1: Build Go binaries
# ---------------------------------------------------------------------------
# Keep this in step with the `go` directive in go.mod. The official images pin
# GOTOOLCHAIN=local, so a base image older than go.mod fails outright at
# `go mod download` rather than fetching a newer toolchain.
FROM golang:1.25-alpine AS builder

# Install build dependencies
RUN apk add --no-cache git

WORKDIR /vision3

COPY go.mod go.sum ./
COPY third_party/ ./third_party/
RUN go mod download

COPY . .

RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /vision3/ViSiON3   ./cmd/vision3
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /vision3/v3mail    ./cmd/v3mail
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /vision3/helper    ./cmd/helper
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /vision3/strings   ./cmd/strings
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /vision3/ue        ./cmd/ue
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /vision3/config    ./cmd/config
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /vision3/menuedit  ./cmd/menuedit
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-w -s" -o /vision3/wfc       ./cmd/wfc

# ---------------------------------------------------------------------------
# Stage 2: Runtime image
# ---------------------------------------------------------------------------
FROM alpine:latest

# Install runtime dependencies. su-exec drops privileges in the entrypoint;
# tzdata makes the TZ environment variable actually take effect.
RUN apk --no-cache add openssh-keygen ca-certificates su-exec tzdata

# Create non-root user for running the BBS
RUN addgroup -S vision3 && adduser -S vision3 -G vision3

WORKDIR /vision3

COPY docker-entrypoint.sh /usr/local/bin/
RUN chmod a+x /usr/local/bin/docker-entrypoint.sh

# Copy all built Go binaries
COPY --from=builder /vision3/ViSiON3   .
COPY --from=builder /vision3/v3mail    .
COPY --from=builder /vision3/helper    .
COPY --from=builder /vision3/strings   .
COPY --from=builder /vision3/ue        .
COPY --from=builder /vision3/config    .
COPY --from=builder /vision3/menuedit  .
COPY --from=builder /vision3/wfc       .

# Copy template configs for initialization (includes sexyz.ini; entrypoint copies it to bin/)
# Note: bin/sexyz and bin/binkd must be provided via a volume or added to a derived image
COPY templates/ ./templates/

# Ship the default menu set so the image runs without a menus/ mount. Mount over
# /vision3/menus only if you keep a customised set on the host.
COPY menus/ ./menus/

# Create the mount points before declaring them so a named volume inherits
# vision3 ownership. Bind mounts still arrive owned by the host uid, which the
# entrypoint corrects at runtime.
RUN mkdir -p /vision3/configs /vision3/data /vision3/temp /vision3/bin \
    && chown -R vision3:vision3 /vision3

VOLUME /vision3/configs
VOLUME /vision3/menus
VOLUME /vision3/data

EXPOSE 2222 2323

# Marks the preflight check's guidance as containerised; see cmd/vision3/preflight.go.
ENV VISION3_CONTAINER=1

# NOTE: deliberately no `USER vision3`. The entrypoint starts as root only long
# enough to chown the mounted volumes, then drops to vision3 via su-exec.
ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

CMD ["./ViSiON3"]
