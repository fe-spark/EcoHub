# syntax=docker/dockerfile:1.7
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS builder

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    TZ=Asia/Shanghai

ARG TARGETARCH

WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY . .
RUN --mount=type=cache,target=/root/.cache/go-build GOARCH=$TARGETARCH go build -o /out/main ./cmd/server/...

FROM alpine:3.22

LABEL maintainer="spark" \
      org.opencontainers.image.title="EcoHub Server" \
      org.opencontainers.image.description="EcoHub Go API server for collection, aggregation, cache, authentication, and open interfaces." \
      org.opencontainers.image.source="https://github.com/fe-spark/EcoHub"

ENV TZ=Asia/Shanghai

RUN apk add --no-cache ca-certificates tzdata wget

WORKDIR /app
COPY --from=builder /out/main ./main

EXPOSE 8080

CMD ["./main"]
