# v2.0.3

> **补丁版**：群聊 `/search` 去掉误入的 `@bot` 提及；前台、登录、后台与 Bot 补上项目地址。正式版会覆盖 `ghcr.io/fe-spark/ecohub:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub:v2.0.3`
- `ghcr.io/fe-spark/ecohub:latest`

## 修复

- **Telegram 群聊 `/search`**：去掉指令参数里的 `@bot` 提及（追加、前置、粘连），避免把机器人用户名当成搜索关键词

## 新增

- **项目地址**：前台页头/页脚、登录页、管理端顶栏与 Bot 欢迎/帮助增加 GitHub 仓库入口

## 部署（v2.0.3）

```bash
# 推荐：安装脚本 + 发布版 Compose（默认 :latest）
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub && docker compose pull && docker compose up -d

# 或固定版本：
#   image: ghcr.io/fe-spark/ecohub:v2.0.3
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。  
全部署方式见 [README-Deploy.md](./README-Deploy.md)。
