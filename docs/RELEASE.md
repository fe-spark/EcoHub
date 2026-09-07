测试版 **v2.6.0-beta.3**，Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.6.0-beta.3`。

### 升级指引

- **从已有版本升级**：
  - **1Panel / Compose**：执行 `docker compose pull ecohub && docker compose up -d ecohub` 即可（或后台「检查更新」一键平滑升级）。
  - **数据结构与兼容性**：完全向下兼容现有 MySQL 与 Redis 数据结构，无破坏性变更。

---

### v2.6.0-beta.3 核心变更

#### 1. 前端路由归一化与 Loading 收尾修复
- 修复 `normalizeAppHref` 对相对路径未做标准 URL 解析与查询参数编码规范化的问题；
- 统一使用 `URLSearchParams` 标准序列化与参数键排序，消除手动 `encodeURIComponent` 与 Next.js `useSearchParams()` 的编码格式差异（如 `%20` vs `+`、未转义括号等）；
- 彻底解决搜索包含空格或特殊字符关键词时，路由比较失配导致全屏 Loading 遮罩挂起卡死 15 秒的严重缺陷。

#### 2. 管理端数据分析入口与鉴权状态收敛
- 统一收敛数据分析页面和导航菜单状态判断，严格依赖后端启用状态；
- 移除未启用时的冗余提示与过渡状态闪烁，保持管理端权限控制与路由守卫逻辑一致。

#### 3. 质量与回归验证
- **全仓测试**：后端全模块单测 100% PASS 通过；
- **前端检查**：Next.js `npm run lint` 与 `npx tsc --noEmit` 均为 0 错误。
