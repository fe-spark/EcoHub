FROM golang:1.24-alpine

LABEL maintainer="spark" \
      org.opencontainers.image.title="EcoHub Server" \
      org.opencontainers.image.description="EcoHub Go API server for collection, aggregation, cache, authentication, and open interfaces." \
      org.opencontainers.image.source="https://github.com/fe-spark/EcoHub"

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    TZ=Asia/Shanghai

RUN apk add --no-cache ca-certificates tzdata

WORKDIR /app

# 先复制依赖声明并下载（独立缓存层，代码变动不会重新下载）
COPY go.mod go.sum ./
RUN go mod download

# 再复制源码并编译
COPY . .

RUN go build -o main ./cmd/server/...

EXPOSE 8080

CMD ["./main"]
