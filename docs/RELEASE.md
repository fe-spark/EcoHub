测试版 **v3.0.2-beta.1**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v3.0.2-beta.1`。

### 升级指引

- **从 v3.0.x 升级**：`docker compose pull ecohub && docker compose up -d ecohub`。
- **v2.x**：不兼容，不迁移。必须全新目录部署，勿挂载旧库。

### v3.0.2-beta.1 变更

- 采集防旧页截断：在来源映射中记录源站 `vod_time` 并引入原子 CAS 条件更新，丢弃小时列表中早于已落库快照的旧数据，彻底解决并发交错写入导致的片单截断与连续采集重复报更问题。
- 子模块指针对齐：同步更新 `app-for-ohos` 客户端子模块指针。
