正式版 **v2.7.6**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.7.6` 与 `ghcr.io/fe-spark/ecohub:latest`。

### 升级指引

- **平滑升级**：后台「检查更新」一键升级，或执行 `docker compose pull ecohub && docker compose up -d ecohub`。
- **数据兼容**：全自动平滑升级，无需手动执行 SQL 或重新初始化配置。

---

### v2.7.6 核心变更

- **采集代理**：支持 HTTP/HTTPS/SOCKS5，可作用于全部或指定采集站。
- **测试可选**：代理开启时，测试接口可选走代理或直连。
- **密码不回显**：读取代理配置不返回账号密码。
- **搜索提示**：源站不支持搜索或只返回计数时，给出明确说明。
