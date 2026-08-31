测试版（Pre-release），Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.0-beta.11`。

### 核心变更

#### 1. 访问分析热门点播影视海报只读模型优先级反查与主动缓存淘汰
- **三级只读模型优先级反查**：访问分析热门点播看板（`resolveFilmMetas`）彻底废弃直接查询原始采集表 `film_index` 的旧逻辑，重构为优先命中活跃快照表 `FilmListSnapshot`（包含最新自定义封面与海报源封面），次优查询 `movie_details`（`DisplayPicture()`），最后兜底 `film_index`，彻底解决自定义或换源后热播榜显示旧图的问题。
- **保存与删除即时精准淘汰**：在影片录入/编辑（`SaveFilmDetail`）与删除（`DelFilm`）成功后，立即调用 `access.InvalidateFilmMetaCache` 主动失效对应影片的元数据内存缓存，并将兜底 TTL 优化缩短至 1 分钟。

#### 2. MacCMS / TVBox 接口相对图片路径智能归一化与高效注入
- **高效 URL 归一化**：在 MacCMS / TVBox 接口（`provide_handler.go`）中新增高性能 `normalizeMediaURL` 判定逻辑，优先跳过已带有 `http(s)://` 的绝对地址，仅对以 `/` 开头的相对海报路径自动结合请求 Host / X-Forwarded-Host 补全域名，保障第三方播放器稳定展示图片。

#### 3. 后台全场景素材选图与上传全链路自动补全域名
- **通用素材选图器域名自动补齐**：后台素材选择弹窗（`ImagePicker`）在选中图片并确认时，自动将相对路径（`/api/upload/pic/poster/...`）转换为携带当前访问域名的完整 URL（`window.location.origin`），彻底避免填充无域名的绝对路径。
- **影片管理与首页轮播上传对齐**：优化影片编辑（`film/add`）与轮播管理（`banners`）中的直接上传（`Upload`）逻辑，上传成功后自动补全当前站点协议与域名。
- **系统基础配置与赞赏渠道优化**：网站 Logo、赞赏渠道收款码选图均对齐完整域名回显与提交逻辑。
- **素材中心链接复制优化**：素材中心卡片操作栏点击“复制链接”时，自动复制带域名的完整绝对 URL。

#### 4. 客户端（Android & 鸿蒙）相对媒体路径多重兜底解析
- **Android 端统一海报解析**：`FormatUtil.poster`、`bannerPoster`、`bannerBackdrop` 底层全面接入 `ServerConfigManager.resolveMediaUrl`，自动根据当前配置的软件源基地址补全历史相对图片路径。
- **鸿蒙端统一海报解析**：`FormatUtil` 各海报与轮播图解析函数全面接入 `ServerConfigManager.resolveMediaUrl`，确保多端体验一致。
