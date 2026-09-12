正式版 **v2.6.3**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.6.3` 与 `ghcr.io/fe-spark/ecohub:latest`。

### 升级指引

- **从已有版本升级**：
  - **1Panel / Compose**：执行 `docker compose pull ecohub && docker compose up -d ecohub` 即可（或后台「检查更新」一键平滑升级）。
  - **数据结构与兼容性**：完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。

---

### v2.6.3 核心变更

#### 1. 容器平滑升级与编排链路深度加固
- **Docker API 泛化兼容**：移除硬编码的 `/v1.43` API 前缀，全面兼容 Docker < 24.0 及主流 NAS（群晖 Synology、威联通 QNAP、TrueNAS 等系统自带的 Docker 20.10.x），杜绝 400 版本不匹配阻断；
- **多网络拓扑保序支持**：解决 Go map 迭代随机性导致的多网络顺序漂移问题，自动提取并锁定主网络（匹配 `HostConfig.NetworkMode`）放入创建请求，其余网络通过独立 API 追加连接，避免 Docker 400 网络冲突；
- **升级助手状态自愈与回滚**：向升级助手传递原容器名，在启动新容器失败或旧容器未退出超时时，自动清理残余容器并将旧容器原子恢复原名拉起，彻底避免容器名被污染或死锁孤立；
- **停机信号窗口与平滑排水**：`supervisord.conf` 增加 `stopwaitsecs=25`，Docker 停机等待时间增至 `t=30`，确保爬虫写队列排空落盘与通知发布完全收尾；助手侧兜底触发 `stop`，防止请求中断时被动等待超时；
- **环境路径与容器识别**：动态继承宿主机 Docker Socket 挂载路径（支持 Rootless Docker 等自定义路径），扩充 cgroupfs 驱动下的容器 ID 正则匹配并支持默认容器名 fallback；
- **前端轮询与交互体验**：前端轮询拉取时长对齐后端 12 分钟超时，提示文案展示实际目标版本，支持页面刷新后自动接续升级检测。

#### 2. 前端移动端与弹窗交互修复
- **移动端水平溢出处理**：全局样式与公共布局补充 `overflow-x: clip` 与 `overflow-x: hidden`，修复小屏移动端横向晃动；
- **通知与打赏弹窗居中与自适应**：优化 NoticeModal 与 TipModal 居中展示，限制移动端最大宽度（480px / 520px），防止弹窗偏移或内容截断。
