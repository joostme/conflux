FROM golang:1.23-alpine AS builder

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 go build -ldflags="-s -w" -o conflux .

FROM alpine:3.20

RUN apk add --no-cache \
    age \
    curl \
    && SOPS_VERSION="3.9.4" \
    && ARCH="$(uname -m)" \
    && case "$ARCH" in \
         x86_64)  SOPS_ARCH="amd64" ;; \
         aarch64) SOPS_ARCH="arm64" ;; \
         *)       echo "unsupported arch: $ARCH" && exit 1 ;; \
       esac \
    && curl -fsSL "https://github.com/getsops/sops/releases/download/v${SOPS_VERSION}/sops-v${SOPS_VERSION}.linux.${SOPS_ARCH}" \
       -o /usr/local/bin/sops \
    && chmod +x /usr/local/bin/sops \
    && mkdir -p /data/repo /data/work

COPY --from=builder /build/conflux /usr/local/bin/conflux

ENTRYPOINT ["/usr/local/bin/conflux"]
