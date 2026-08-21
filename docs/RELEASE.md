测试版 **v2.1.2-beta.2**，镜像 `ghcr.io/fe-spark/ecohub:v2.1.2-beta.2`，**不会**覆盖 `:latest`。

已部署 **v2.1.0** 及以上：把 compose 镜像改成 `ghcr.io/fe-spark/ecohub:v2.1.2-beta.2` 后执行 `docker compose pull && docker compose up -d`。正式版后台不会把 beta 当作可升级版本。

从 **v2.1.0 之前** 升级的，请先按 [v2.1.0 说明](https://github.com/fe-spark/EcoHub/releases/tag/v2.1.0) 做完素材卷迁移，再升到本版本。

### 本版本变更

- 鸿蒙客户端播放器：修复控件无法点击、暂停后仍播放、失败后重试卡住、切源后仍显示失败等问题
- 播放器 HUD 改为移动端布局；倍速最高 3x，长按 3x；倍速在屏幕底部弹窗选择
- 全屏顶栏「上集 / 下集」为文字图标按钮
- 观看历史按软件源分桶存储，切源不再串数据
- 首页推荐 / 更新 tab 切换时不再自动刷新
