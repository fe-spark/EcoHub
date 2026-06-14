# ---- Base Stage ----
FROM --platform=$BUILDPLATFORM node:20-alpine AS build-base

# ---- Dependencies Stage ----
FROM build-base AS deps
RUN apk add --no-cache libc6-compat
WORKDIR /app
COPY package.json package-lock.json* ./
RUN --mount=type=cache,target=/root/.npm \
    npm install

# ---- Builder Stage ----
FROM build-base AS builder
WORKDIR /app
COPY --from=deps /app/node_modules ./node_modules
COPY . .

ENV NEXT_TELEMETRY_DISABLED=1

ARG API_URL
ENV API_URL=${API_URL}

RUN --mount=type=cache,target=/app/.next/cache \
    npm run build

# ---- Runner Stage ----
FROM node:20-alpine AS runner
WORKDIR /app

LABEL org.opencontainers.image.title="EcoHub Web" \
      org.opencontainers.image.description="EcoHub Next.js frontend for the public site, login page, and management console." \
      org.opencontainers.image.source="https://github.com/fe-spark/EcoHub"

ENV NODE_ENV=production
ENV NEXT_TELEMETRY_DISABLED=1

# 运行时需要传入 API_URL，供 SSR 服务端渲染调用后端接口
ARG API_URL
ENV API_URL=${API_URL}

RUN addgroup --system --gid 1001 nodejs
RUN adduser --system --uid 1001 nextjs

COPY --from=builder /app/public ./public
COPY --from=builder --chown=nextjs:nodejs /app/.next/standalone ./
COPY --from=builder --chown=nextjs:nodejs /app/.next/static ./.next/static

USER nextjs

EXPOSE 3000
ENV PORT=3000
ENV HOSTNAME="0.0.0.0"

CMD ["node", "server.js"]
