正式版 **v2.5.3**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.3` 与 `ghcr.io/fe-spark/ecohub:latest`。

### 升级指引

- **从已有版本升级**：执行 `docker compose pull && docker compose up -d` 即可（或后台「检查更新」一键平滑升级）。
- **兼容性说明**：本版本完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。

---

### v2.5.3 核心变更

#### 1. 鸿蒙端（OHOS）客户端底层播放架构重构 (v1.1.5)
- **原生 AVPlayer 与 Surface 渲染重构**：鸿蒙客户端全面重构为 `PlayerAv` 播放引擎（基于 `media.createAVPlayer` 与 `XComponent` 硬件渲染），彻底解决视频非标分辨率画面拉伸与多码率解码兼容性问题。
- **全屏横竖屏智能自适应**：动态感知视频原始宽高比（`videoSizeChange`），自动识别短剧/竖屏与横屏电影，智能调度 `PORTRAIT` 与 `AUTO_ROTATION_LANDSCAPE`。
- **选集与详情抽屉化交互重构**：基于动态吸顶高度测量精确推移剧集位置，新增 `FilmDetailDialog` 完整呈现影片简介与演职员信息。
- **离线断网智能感知与全局换源重置**：引入 `@kit.NetworkKit` 前置离线检测挂起探活；换源成功后清空旧路由栈（`router.clear()`），安全重置全局代数。

#### 2. 安卓端（Android / Flutter）客户端图标与多端资源同步
- **高清应用图标与启动图全套更新**：同步全新极简品牌视觉，适配 Android mipmap 各 DPI 分辨率及 iOS AppIcon 尺寸规范。

#### 3. 网页端与全局品牌 Logo 焕新
- **全局 Logo 资源更新**：更新主站 Logo 及各端媒体资源，提升高分屏下图标清晰度与视觉质感。
