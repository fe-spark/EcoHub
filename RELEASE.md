> **破坏性变更**：已部署旧版必须按下面做完再启动。全新安装、本版本之后的升级不必再做。正式版会覆盖 `:latest`。

## 必须执行

会覆盖 `docker-compose.yml`，保留 `.env` 和 `data/`。

旧容器还在且传过素材，先拷到宿主机（否则图会丢）：

```bash
mkdir -p ~/ecohub/data/uploads/gallery
docker cp Eco-hub:/app/server/static/upload/gallery/. ~/ecohub/data/uploads/gallery/
```

然后重新拉安装脚本并启动：

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub
docker compose stop
docker compose pull
docker compose up -d
```

之后有新版本：后台点「立即升级并重启」，或再执行 `stop && pull && up -d`。

## 本版本其它变更

- 素材中心：上传落到发布卷；列表支持时间筛选并始终分页
- 发布版后台可拉 `latest` 并重启当前容器
- 采集结束即可引导下一步；采集站弹窗不再残留上次输入
- 登录页去掉嵌套 ConfigProvider，消除 hydration 报错
