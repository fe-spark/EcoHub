测试版 **v2.6.0-beta.2**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.6.0-beta.2`。

### 升级指引

- **从已有版本升级**：
  - **1Panel / Compose**：执行 `docker compose pull ecohub && docker compose up -d ecohub` 即可（或后台「检查更新」一键平滑升级）。
  - **数据结构与兼容性**：完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。

---

### v2.6.0-beta.2 核心变更

#### 1. 主站切换冷启动保护期 Redis 持久化与安全兜底
- 将主站切换冷启动保护期由单机内存持久化至 Redis（Key: `EcoHub:CleanOrphan:MasterSwitchProtect`，TTL 7天）；
- 杜绝容器重启或多实例部署导致保护期提前失效引发孤儿扫描误删主站播放列表；
- 增加 Redis 异常时的 Fail-Safe 保守安全策略与单机内存读写锁平滑回退。

#### 2. 采集源变更与删除数据治理全链路闭环
- 恢复采集源变更（换源 `isUriChanged`、主站升降级）时的旧失败记录清理，防止使用新接口拉取旧页码产生脏数据；
- 在采集站点物理删除事务中统一调用 `DeleteFailureRecordsByOriginIdTx` 清理关联失败记录；
- 恢复单片影视删除（`DelFilmSearch`）时在同一事务中查出所有 `match_key` 并物理级联清理 `slave_movie_playlists`，杜绝孤儿播放列表残留；
- 修复管理端影片列表删除操作使用的 mid 主键传参（`record.mid || record.ID`）。

#### 3. 冗余死代码清理与滚动水位解析复用
- 移除未被生产引用的 `DefaultTrustedProxies` 与 `ParseTrustedProxies` 死代码，测试覆盖 `parseEnvBool`；
- 在 `loadRolledDay` 中复用 `parseRolledDay` 统一水位解析与 Redis 异常向上传播；
- 校正 PR 说明，移除已收敛按天清理后的虚假手动落库描述。

#### 4. 质量与回归验证
- **全仓测试**：`go test -count=1 ./...` 后端全模块所有 17 个包 100% PASS 通过；
- **前端检查**：Next.js `npm run lint` 与 `npx tsc --noEmit` 均为 0 错误。
