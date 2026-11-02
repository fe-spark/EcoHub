正式版 **v3.0.1**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v3.0.1` 及 `latest`。

### 升级指引

- **从 v3.0.0 升级**：`docker compose pull ecohub && docker compose up -d ecohub`。
- **v2.x**：不兼容，不迁移。必须全新目录部署，勿挂载旧库。
- 本版镜像发布成功后，会删除全部 beta tag 和镜像。固定版本只写正式版 tag。
- 已并进同一档案的同站不同 `vod_id`，下次采集会拆开。最小 `vod_id` 留在原档案，其余各建新档并通知一次。

### v3.0.1 变更

- 站内身份改为采集站 + `vod_id`。同站同名不再收成一条档案，匹配键只用于播放页查找，不再合并档案。
- 正式版镜像推送成功后，自动删除全部 beta Release、tag 和 GHCR 镜像。
- 移动端子模块指针对齐各自 `main`。
