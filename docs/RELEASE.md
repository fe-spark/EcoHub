正式版 **v2.6.5**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.6.5` 与 `ghcr.io/fe-spark/ecohub:latest`。

### 升级指引

- **平滑升级**：支持后台「检查更新」一键平滑升级，或通过 `docker compose pull ecohub && docker compose up -d ecohub` 快速更新。
- **数据兼容**：完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。

---

### 📦 存量匹配键迁移工具（按需执行）

> **提示**：v2.6.5 升级后，所有**新采集/新抓取**的数据会自动按新规范生成带大类的物理唯一匹配键，**日常运行无需强制执行迁移**。  
> 仅当你需要对**存量历史采集的老影片**彻底刷新为全新大类隔离匹配键（防止历史遗留的跨分类同名串台）时，可在升级容器后按需执行一次。

#### 方案一：Docker 生产环境（推荐，一键执行）
本 Release 附件已提供编译好的独立可执行文件（`migrate_match_keys_linux_amd64` 与 `migrate_match_keys_linux_arm64`）。在宿主机终端复制执行以下命令即可，**自动继承容器内数据库配置，无需手动输密码**：

```bash
ARCH=$(uname -m | sed -e 's/x86_64/amd64/' -e 's/aarch64/arm64/') && \
curl -fsSL "https://github.com/fe-spark/EcoHub/releases/download/v2.6.5/migrate_match_keys_linux_${ARCH}" -o /tmp/migrate_match_keys && \
docker cp /tmp/migrate_match_keys Eco-hub:/app/migrate_match_keys && \
docker exec -it Eco-hub chmod +x /app/migrate_match_keys && \
docker exec -it Eco-hub /app/migrate_match_keys
```

*(迁移完成后清理临时文件：`docker exec Eco-hub rm -f /app/migrate_match_keys && rm -f /tmp/migrate_match_keys`)*

#### 方案二：源码/本地环境
```bash
go run ./cmd/tool/migrate_match_keys
```

---

### v2.6.5 核心变更

#### 1. 匹配键大类物理隔离与防串台加固
- **大类单一权威源**：严格以主站采集大类为权威标准，彻底剔除副站猜词臆测逻辑；未识别副站标签规范返回 `0` 并由内存缓存拦截，0 SQL 穿透。
- **物理实体 PrimaryKey 单键存储**：`slave_movie_playlist` 与 `movie_poster` 严格按首选主键（`hash(title#cat_pid)`）落库，杜绝副站短剧生成纯片名记录，彻底根除前台同名跨大类剧集（如动漫与短剧）串台污染漏洞。
- **表体积与写 I/O 减半**：物理播放列表移除多键平铺写入，行数与磁盘空间减少 50%~66%，大幅降低大表事务锁持有时间与自增 ID 消耗。

#### 2. 主站检索与候选探测双轨兼容
- **存量平滑过渡**：主站详情检索与候选探测端保留 `[dbid, cat_pid, title]` 双轨回退键，确保未识别分类的通用副站源与存量未迁移旧片正常召回。
- **候选归一化比对**：候选匹配时对存量子分类自动归一化解析为根大类，避免历史数据结构差异导致合法大类副站源被误拒。

#### 3. 并发安全与高可用修复
- **递归死锁彻底消除**：重构 `category_support` 中的读写锁竞争链路，将并发安全的 `sync.Map` 遍历移至 `catMu` 锁外执行，杜绝高并发采集与后台刷新分类缓存时的确定性死锁隐患。

#### 4. 千万级数据迁移工具加固
- **流式游标分页**：`migrate_match_keys` 脚本全面改用基于主键的 Keyset Pagination 游标分页（`WHERE id > ? ORDER BY id ASC LIMIT ?`），单批次内存恒定保持 O(1)，杜绝大表全表扫描 OOM 崩溃。
