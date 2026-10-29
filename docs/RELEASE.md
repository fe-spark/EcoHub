预发布版 **v3.0.0-beta.19**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v3.0.0-beta.19`。不会覆盖 `latest`。

### 升级指引

- **试用本版**：把 Compose 镜像版本指定为 `ghcr.io/fe-spark/ecohub:v3.0.0-beta.19`，然后执行 `docker compose pull ecohub && docker compose up -d ecohub`。
- **破坏性变更与数据兼容**：**不兼容，不迁移。** 本版本为 v3.0 重大架构重构版本，完全不向后兼容 v2.x 历史数据。已入库的播放列表也不会改写；重复选集要重新采集对应站点才会拆开。请勿直接挂载 v2.x 历史数据库升级。

---

### v3.0.0-beta.19 核心变更

- **异常重试跳过原因精准分类与细化明细日志**：
  - 失败记录入队检查细化分类统计停用（`Disabled`）、采集中（`Busy`）、不存在（`NotFound`）等具体原因，控制台与系统日志完整输出被跳过记录的源站名称、源站 ID 与页码；
  - 彻底替换模糊的混合提示文案，按实际原因精准反馈（如「4 条未开始：采集站已停用」），解决无采集运行时提示正在采集的误解。
- **彻底解决采集完成后立即执行计划任务误判采集中（占用锁残留）缺陷**：
  - 终态站点即时解除占用：当站点在进度状态中已进入 `StatusDone`、`StatusStopped` 或 `StatusFailed` 终态且无任务运行，系统在检查重试阻断（`failureRecoverBlocked`）与占用查询（`isOccupiedCollectSource`）时即刻自动清除残留占用锁，零延迟消除假占用；
  - `releaseSourceOccupy` 完善兜底释放：当批次占用令牌丢失或不匹配时，自动降级按站点 ID 强制释放 `collectingSources`，杜绝单站采集完成退出后内存占用锁残留；
  - 站点 ID 统一修剪空白字符，采集队列 ID 分配异常时增加安全回滚释放。
