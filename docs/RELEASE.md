测试版 **v2.4.0-beta.1**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.4.0-beta.1`。

### 升级指引

- **从已有版本升级**：拉取镜像 `ghcr.io/fe-spark/ecohub:v2.4.0-beta.1` 或配置 docker-compose 测试升级。本版本为测试版，不会覆盖 `:latest`。

---

### v2.4.0-beta.1 核心变更

#### 1. 访问分析（双轨采集）
- **页面画像**：Web / 鸿蒙通过 `POST /api/stat/view` 上报浏览、搜索、播放、分类；PV / UV / 行为 / 热搜 / 热播只计页面浏览，不再把每个 API 当 PV。
- **HTTP 健康度**：中间件只记延迟、4xx/5xx、慢请求、热路径与访问日志；TVBox Provide 单独计数，不计入站点四格。
- **隐私**：不落完整 IP，UV 用 HMAC 进 HyperLogLog；公开埋点限制 4KB，热搜/热播 ZSET 只留 Top 200。
- **后台**：新增「访问分析」页（近 14 天、日志分页）。

#### 2. 鸿蒙客户端 (v1.1.2-beta.1)
- 首页 / 搜索 / 播放 / 分类埋点，User-Agent `EcoHub-OHOS/{version}`。
- 修复 ArkTS 无类型对象字面量导致的埋点编译失败。
