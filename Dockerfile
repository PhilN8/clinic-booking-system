# Multi-stage build. The builder compiles a static binary; the runtime image
# carries only the binary and the certificates it needs.

FROM golang:1.25-alpine AS builder

WORKDIR /src

# Download dependencies first so that they are cached independently of the
# source code.
COPY go.mod go.sum ./
RUN go mod download

COPY . .

# CGO is disabled so the result is a static binary that runs on a minimal
# base image. The IANA timezone database is embedded into the binary itself
# (see the time/tzdata import in cmd/api/main.go), so the runtime image does
# not need tzdata either.
RUN CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w" \
    -o /out/api ./cmd/api

FROM alpine:3.20

RUN apk add --no-cache ca-certificates \
    && addgroup -S app \
    && adduser -S -G app app

COPY --from=builder /out/api /usr/local/bin/api

USER app

EXPOSE 8080

ENTRYPOINT ["/usr/local/bin/api"]
