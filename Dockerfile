# syntax=docker/dockerfile:1.7

# ==========================================
# 1. 编译 Go 后端服务 (server)
# ==========================================
FROM --platform=$BUILDPLATFORM golang:1.24-alpine AS server-builder

ENV GO111MODULE=on \
    CGO_ENABLED=0 \
    GOOS=linux \
    TZ=Asia/Shanghai

ARG TARGETARCH

WORKDIR /src
COPY server/go.mod server/go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download

COPY server/ .
RUN --mount=type=cache,target=/root/.cache/go-build GOARCH=$TARGETARCH go build -o /out/main ./cmd/server/...

# ==========================================
# 2. 编译 Next.js 前端应用 (web)
# ==========================================
FROM --platform=$BUILDPLATFORM node:20-alpine AS web-deps
RUN apk add --no-cache libc6-compat
WORKDIR /app
COPY web/package.json web/package-lock.json ./
RUN --mount=type=cache,target=/root/.npm npm ci

FROM --platform=$BUILDPLATFORM node:20-alpine AS web-builder
WORKDIR /app
COPY --from=web-deps /app/node_modules ./node_modules
COPY web/ .

ENV NEXT_TELEMETRY_DISABLED=1
ARG API_URL=http://127.0.0.1:8080
ENV API_URL=${API_URL}

RUN --mount=type=cache,target=/app/.next/cache npm run build

# ==========================================
# 3. 运行环境 (Runner): 合并为一个镜像
# ==========================================
FROM node:20-alpine AS runner

LABEL org.opencontainers.image.title="EcoHub All-in-One" \
      org.opencontainers.image.description="EcoHub All-in-One single image containing both Go server and Next.js web." \
      org.opencontainers.image.source="https://github.com/fe-spark/EcoHub"

ENV NODE_ENV=production \
    NEXT_TELEMETRY_DISABLED=1 \
    TZ=Asia/Shanghai \
    PORT=8080 \
    API_URL=http://127.0.0.1:8080

RUN apk add --no-cache ca-certificates tzdata supervisor

WORKDIR /app

# 拷贝 Go 服务二进制文件
COPY --from=server-builder /out/main /app/server/main

# 拷贝 Next.js 产物
COPY --from=web-builder /app/public /app/web/public
COPY --from=web-builder /app/.next/standalone /app/web/
COPY --from=web-builder /app/.next/static /app/web/.next/static

# Supervisord 进程配置与启动脚本
COPY supervisord.conf /etc/supervisord.conf
COPY supervisord-worker.conf /etc/supervisord-worker.conf
COPY entrypoint.sh /app/entrypoint.sh
RUN chmod +x /app/entrypoint.sh

EXPOSE 3000 8080

ENTRYPOINT ["/app/entrypoint.sh"]
