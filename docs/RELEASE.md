测试版（Pre-release），Docker 镜像 `ghcr.io/fe-spark/ecohub:v2.5.0-beta.10`。

### 核心变更

#### 1. 后台素材选图与上传全链路自动补全域名
- **通用素材选图器域名自动补齐**：后台素材选择弹窗（`ImagePicker`）在选中图片并确认时，自动将相对路径（`/api/upload/pic/poster/...`）转换为携带当前访问域名的完整 URL（`window.location.origin`），彻底避免填充无域名的相对/绝对路径。
- **影片管理与首页轮播上传对齐**：优化影片编辑（`film/add`）与轮播管理（`banners`）中的直接上传（`Upload`）逻辑，上传成功后自动补全当前站点协议与域名。
- **系统基础配置与赞赏渠道优化**：网站 Logo、赞赏渠道收款码选图均对齐完整域名回显与提交逻辑。
- **素材中心链接复制优化**：素材中心卡片操作栏点击“复制链接”时，自动复制带域名的完整绝对 URL。

#### 2. MacCMS / TVBox 接口相对图片路径服务端智能补全
- **提供端 `/provide/vod` 域名注入**：在向 TVBox / 影视仓等外部客户端输出影片列表与详情数据时，服务端自动识别以 `/` 开头的相对海报路径，并基于当前请求的 Host / X-Forwarded-Host 自动注入完整域名，确保第三方播放器与客户端均能稳定加载海报。

#### 3. 客户端（Android & 鸿蒙）相对媒体路径多重兜底解析
- **Android 端统一海报解析**：`FormatUtil.poster`、`bannerPoster`、`bannerBackdrop` 底层全面接入 `ServerConfigManager.resolveMediaUrl`，自动根据当前配置的软件源基地址补全历史相对图片路径。
- **鸿蒙端统一海报解析**：`FormatUtil` 各海报与轮播图解析函数全面接入 `ServerConfigManager.resolveMediaUrl`，确保多端体验一致。
