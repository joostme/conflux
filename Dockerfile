FROM golang:1.26-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o conflux .

FROM alpine:3.20

# All external tool dependencies have been eliminated:
# - Git operations use go-git (pure Go) — no git CLI needed
# - SOPS decryption uses getsops/sops/v3 library — no sops CLI needed
# - Age key handling uses filippo.io/age library — no age CLI needed
# Only ca-certificates is needed for TLS (git clone over SSH/HTTPS).
RUN apk add --no-cache ca-certificates \
    && mkdir -p /data/repo /data/work

COPY --from=builder /build/conflux /usr/local/bin/conflux

ENTRYPOINT ["/usr/local/bin/conflux"]
