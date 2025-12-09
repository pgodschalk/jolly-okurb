# Build stage
FROM golang:1.25-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
  go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
  --mount=type=cache,target=/root/.cache/go-build \
  CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o /usr/local/bin/jolly-okurb ./cmd/bot

# Runtime stage
FROM alpine:3.23

RUN --mount=type=cache,target=/var/cache/apk \
  apk add \
  ca-certificates \
  tzdata

COPY --from=builder /usr/local/bin/jolly-okurb /usr/local/bin/jolly-okurb

USER nobody:nobody

ENTRYPOINT ["jolly-okurb"]
