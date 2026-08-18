> **破坏性改动**：素材上传改走发布卷。仅**已部署旧版**升级时，须先拷文件再 pull。全新安装不必执行。正式版会覆盖 `:latest`。

## 破坏性改动（仅已部署旧版，执行一次）

素材文件改为写入发布卷 `data/uploads`（容器内 `/app/static/upload/gallery`）。此前 All-in-One 写在容器可写层 `/app/server/static/upload/gallery`。**已部署旧镜像的实例若直接 `docker compose pull && up -d`，会丢掉已上传素材**（MySQL `files` 记录还在，素材中心裂图）。影片、站点配置、账号不受影响。

适用范围：

- **要做**：已经在跑会把素材写到 `/app/server/static/upload` 的旧镜像。只做一次，且必须在旧容器还在时做。
- **不做**：全新安装；已经是本版本之后的升级。

```bash
# 仍在跑旧镜像时执行
docker exec Eco-hub sh -c 'if [ -d /app/server/static/upload/gallery ]; then mkdir -p /app/static/upload/gallery && cp -an /app/server/static/upload/gallery/. /app/static/upload/gallery/ && echo 已拷到卷 src=$(find /app/server/static/upload/gallery -type f | wc -l) dst=$(find /app/static/upload/gallery -type f | wc -l)，请到素材中心确认图片可显示; else echo 旧目录不存在：若尚未 pull 则无需迁移（新装或从未上传）；若已经 pull 过，文件可能已丢，须重新上传; fi'
docker compose pull && docker compose up -d
```

源码版 `Eco-server` 此前若已有上传、且还没挂 `/app/static/upload` 卷，同样只做一次：

```bash
if docker exec Eco-server test -d /app/static/upload/gallery; then
  docker cp Eco-server:/app/static/upload/gallery ./_gallery_bak
  docker compose up --build -d
  docker cp ./_gallery_bak/. Eco-server:/app/static/upload/gallery/
else
  echo "旧目录不存在：若尚未重建则无需迁移；若已经 up 过，文件可能已丢，须重新上传"
fi
```

## 修复

- **素材中心**：用户上传落到发布卷，升级重建容器不再丢图
