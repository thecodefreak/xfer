FROM golang:1.24-alpine AS builder

WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY . .

ARG TARGETOS
ARG TARGETARCH
ARG TARGETVARIANT

RUN set -eux; \
    export CGO_ENABLED=0; \
    export GOOS="${TARGETOS}"; \
    export GOARCH="${TARGETARCH}"; \
    if [ "${TARGETARCH}" = "arm" ] && [ -n "${TARGETVARIANT}" ]; then \
      export GOARM="${TARGETVARIANT#v}"; \
    fi; \
    go build -trimpath -ldflags "-s -w" -o /out/xfer-server ./cmd/xfer-server

FROM alpine:3.20

RUN adduser -D -u 10001 xfer

COPY --from=builder /out/xfer-server /usr/local/bin/xfer-server

USER xfer
EXPOSE 8080

ENTRYPOINT ["xfer-server"]
