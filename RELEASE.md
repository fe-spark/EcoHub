测试版 **v2.4.0-beta.2**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.4.0-beta.2`。

### 升级指引

- **从已有版本升级**：拉取镜像 `ghcr.io/fe-spark/ecohub:v2.4.0-beta.2` 或配置 docker-compose 测试升级。本版本为测试版，不会覆盖 `:latest`。

---

### v2.4.0-beta.2 核心变更

- **访问日志去 SSR 噪声**：`EcoHub-SSR`（Next 服务端直连 Go）的 2xx 不再写入「访问日志 → 全部」，避免刷满 `local` IP。
- **健康度仍采样 SSR**：P95 / 4xx/5xx 直方图照记；慢请求与错误列表仍保留 SSR 异常。

相对 **v2.4.0-beta.1**：访问分析双轨采集与鸿蒙埋点（v1.1.2-beta.1）不变。
