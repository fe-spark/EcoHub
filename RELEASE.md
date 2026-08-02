# v1.1.3

## 修复

- 移动端 Header 遮挡页面内容（顶部留白不足）
- 布局横向溢出导致的横向滚动条
- 搜索页导航遮罩误挡输入框

## 优化

- 全站导航加载反馈：分类/首页显示加载遮罩，遮罩期间锁定移动端滚动
- 筛选页横向滚动：恢复单行滚动，PC 端溢出时显示左右箭头
- 首页 Hero 轮播样式

## 新增

- TopLoadingBar 强制结束导航加载能力
- 各前台页面即时导航加载反馈

## 修改

- 发布版 Docker 镜像改为 `latest`（`ecohub-web` / `ecohub-server`）

## 部署

```bash
curl -fsSL https://raw.githubusercontent.com/fe-spark/EcoHub/main/scripts/install-release.sh | sh
cd ~/ecohub && docker compose up -d
```

默认账号：`admin / admin`（管理员）、`guest / guest`（只读）。正式部署请修改密码与 `JWT_SECRET`。
