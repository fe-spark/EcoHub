# v2.0.4

> **补丁版**：管理后台增加首次采集引导；站点品牌保存后即时生效，并修正首页每日更新换批过渡。正式版会覆盖 `ghcr.io/fe-spark/ecohub:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub:v2.0.4`
- `ghcr.io/fe-spark/ecohub:latest`

## 新增

- **后台新手引导**：首次进入管理后台时，按采集中心 → 全选 / 批量启用 / 批量采集 → 分类、规则、计划任务、失败记录、影片的路径走通；只读账号不自动弹出，可从用户菜单回放

## 修复

- **站点品牌**：网站配置保存后页头 / 侧栏 Logo 与站点名即时刷新，标签图标走后台 Logo
- **每日更新**：修正首页换批过渡，避免批次切换时的闪动或衔接错误

## 部署（v2.0.4）

```bash
# 推荐：安装脚本 + 发布版 Compose（默认 :latest）
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub && docker compose pull && docker compose up -d

# 或固定版本：
#   image: ghcr.io/fe-spark/ecohub:v2.0.4
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。  
全部署方式见 [README-Deploy.md](./README-Deploy.md)。
