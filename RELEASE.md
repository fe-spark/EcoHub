# v2.1.0

> **破坏性改动**：素材上传改走发布卷。从旧 All-in-One 升级**必须先拷文件，再 pull**。直接 `docker compose pull && up -d` 会丢掉已上传素材。正式版会覆盖 `ghcr.io/fe-spark/ecohub:latest`。

镜像：

- `ghcr.io/fe-spark/ecohub:v2.1.0`
- `ghcr.io/fe-spark/ecohub:latest`

## 破坏性改动（必读）

素材文件改为写入发布卷 `data/uploads`（容器内 `/app/static/upload/gallery`）。此前 All-in-One 写在容器可写层 `/app/server/static/upload/gallery`。**直接 `docker compose pull && up -d` 会丢掉已上传素材**（MySQL `files` 记录还在，素材中心裂图）。影片、站点配置、账号不受影响。

从旧镜像升级时，**必须先在仍运行旧镜像的容器上执行**，再拉新镜像（只做一次；本版本之后不必再做）：

```bash
docker exec Eco-hub sh -c 'if [ -d /app/server/static/upload/gallery ]; then mkdir -p /app/static/upload/gallery && cp -an /app/server/static/upload/gallery/. /app/static/upload/gallery/; echo 已拷到卷; else echo 无需迁移（旧目录不存在或已是新布局）; fi'
```

源码版 `Eco-server`（此前无上传卷）先把图拷出再 `up`，否则空 volume 会盖住容器层旧文件：

```bash
docker cp Eco-server:/app/static/upload/gallery ./_gallery_bak
docker compose up --build -d
docker cp ./_gallery_bak/. Eco-server:/app/static/upload/gallery/
```

## 修复

- **素材中心**：用户上传落到发布卷，升级重建容器不再丢图

## 部署（v2.1.0）

```bash
# 已有旧版 All-in-One：先拷素材到卷（旧目录不存在会提示并跳过）
docker exec Eco-hub sh -c 'if [ -d /app/server/static/upload/gallery ]; then mkdir -p /app/static/upload/gallery && cp -an /app/server/static/upload/gallery/. /app/static/upload/gallery/; echo 已拷到卷; else echo 无需迁移（旧目录不存在或已是新布局）; fi'

# 推荐：安装脚本 + 发布版 Compose（默认 :latest）
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub && docker compose pull && docker compose up -d

# 或固定版本：
#   image: ghcr.io/fe-spark/ecohub:v2.1.0
```

默认账号：`admin / admin`、`guest / guest`。正式部署请改密码与 `JWT_SECRET`。  
全部署方式见 [README-Deploy.md](./README-Deploy.md)。
