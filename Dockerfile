# docker run -d -p 2222:2222 -p 2323:2323 \
#   -v "$(pwd)/configs:/vision3/configs" \
#   -v "$(pwd)/data:/vision3/data" \
#   -v "$(pwd)/menus:/vision3/menus" \
#   vision3

# ---------------------------------------------------------------------------
# Stage 1: Build Go binaries
# ---------------------------------------------------------------------------
FROM golang:1.24-alpine AS builder

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

# ---------------------------------------------------------------------------
# Stage 2: Runtime image
# ---------------------------------------------------------------------------
FROM alpine:latest

# Install runtime dependencies
RUN apk --no-cache add openssh-keygen ca-certificates

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

# Copy template configs for initialization (includes sexyz.ini; entrypoint copies it to bin/)
# Note: bin/sexyz and bin/binkd must be provided via a volume or added to a derived image
COPY templates/ ./templates/

RUN chown -R vision3:vision3 /vision3

VOLUME /vision3/configs
VOLUME /vision3/menus
VOLUME /vision3/data

EXPOSE 2222 2323

USER vision3

ENTRYPOINT ["/usr/local/bin/docker-entrypoint.sh"]

CMD ["./ViSiON3"]
