FROM --platform=$BUILDPLATFORM golang:1.26-alpine AS builder

ARG TARGETOS=linux
ARG TARGETARCH

WORKDIR /build
COPY go.mod go.sum ./
RUN go mod download
COPY . .
RUN CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -ldflags="-s -w" -o conflux .

FROM alpine:3.20

RUN apk add --no-cache ca-certificates \
    && mkdir -p /data/repo /data/work

COPY --from=builder /build/conflux /usr/local/bin/conflux

ENTRYPOINT ["/usr/local/bin/conflux"]
