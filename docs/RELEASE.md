正式版 **v2.6.2**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.6.2` 与 `ghcr.io/fe-spark/ecohub:latest`。

### 升级指引

- **从已有版本升级**：
  - **1Panel / Compose**：执行 `docker compose pull ecohub && docker compose up -d ecohub` 即可（或后台「检查更新」一键平滑升级）。
  - **数据结构与兼容性**：完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。

---

### v2.6.2 核心变更

#### 1. 容器平滑升级助手容错与回滚加固
- 过滤容器网络配置中的 `IPv6Gateway`、`GlobalIPv6Address`、`GlobalIPv6PrefixLen`、`DNSNames` 等只读字段，避免 Docker API 创建新容器时校验失败；
- 升级助手进程增加 `ECOHUB_UPGRADE_OLD` / `ECOHUB_UPGRADE_NEW` 环境变量回退，并在启动时跳过 MySQL/Redis 连通等待以加速替换流程；
- 当新容器启动失败时自动回滚重新拉起旧容器，保障生产可用性。

#### 2. 客户端与子模块更新
- **Android 客户端**：优化源配置页面居中布局与 LOGO 展示尺寸（竖屏 88px / 横屏 72px），完善首页轮播无缝循环与播放详情响应式链路；
- **OHOS 客户端**：同步文档与设置中心描述修订。
