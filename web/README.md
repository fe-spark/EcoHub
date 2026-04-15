# Web

`web/` 是 EcoHub 的 Next.js 前端，包含：

- 前台站点
- 登录页
- 管理后台

## 前端结构图

```mermaid
flowchart LR
    Public["(public) 前台页面"] --> ServerApi["src/lib/server-api.ts"]
    Public --> ClientUi["客户端交互组件"]
    ClientUi --> ClientApi["src/lib/client-api.ts"]
    Manage["manage 后台页面"] --> ClientApi
    Login["login 登录页"] --> ClientApi
    ServerApi --> Backend["Go API"]
    ClientApi --> Backend
```

## 技术栈

- Next.js 16.1.6
- React 19.2.3
- TypeScript
- Ant Design 6
- Axios
- Less / CSS Modules
- Artplayer / Hls.js

## 本地运行

### 1. 安装依赖

```bash
cd web
npm install
```

### 2. 配置 `API_URL`

`API_URL` 现在是可选项。

前端开发服务端口与后端 API 端口不是同一个概念：

- 根目录 `.env` 中的 `SERVER_PORT` 用于后端 API
- `./run-web.sh` 默认把 Next 开发服务启动在 `3000`
- 如需自定义前端开发端口，可在根目录 `.env` 中设置 `WEB_PORT`

本地开发默认会根据根目录 `.env` 中的 `SERVER_PORT` 注入前端运行时，并据此推导后端地址：

```env
http://127.0.0.1:$SERVER_PORT
```

只有在 Docker、反向代理或跨机器访问时，才需要显式配置：

```env
API_URL=http://your-api-origin
```

- 配置后会优先使用 `API_URL`
- 未配置时，浏览器端会复用当前页面的协议和主机名，只切换到根目录 `.env` 中的 `SERVER_PORT`
- 未配置时，服务端取数会回退到 `http://127.0.0.1:$SERVER_PORT`
- 当前实现已拆分 `server-api` 与 `client-api`，分别服务于服务端取数和客户端交互

### 3. 启动开发环境

```bash
./run-web.sh
```

前台入口跟随 Next 开发服务地址，后台路径固定为 `/manage`，登录页为 `/login`。

## `API_URL` 在当前实现里的作用

前端代码中的请求会按职责分层后走后端绝对地址：

- 公共内容页优先在 Server Component 中通过 `src/lib/server-api.ts` 取数
- 客户端交互与后台请求通过 `src/lib/client-api.ts` 发起
- 若配置了 `API_URL`，服务端与浏览器端都会优先使用它
- 若未配置，浏览器端复用当前主机名，服务端回退到 `http://127.0.0.1:$SERVER_PORT`

```mermaid
sequenceDiagram
    participant Browser
    participant Next as Next.js
    participant API as Go API

    Browser->>API: 请求当前主机名或 API_URL 对应地址 + /api/index
    API-->>Browser: 返回 JSON
```

## 目录结构

```text
web/
├── src/app/
│   ├── (public)/           # 前台页面
│   ├── login/              # 登录页
│   └── manage/             # 后台页面
├── src/components/         # 业务组件
├── src/lib/                # API 封装、消息、公共逻辑
├── src/proxy.ts            # /manage 路由预拦截
├── next.config.ts          # Next 构建配置
└── package.json
```

## 请求与鉴权

### API 请求

- `(public)` 目录下的内容页默认使用 Server Component 做首屏取数
- `manage`、`login`、`play` 等强交互页面继续使用 Client Component
- `src/lib/server-api.ts` 负责服务端读接口
- `src/lib/client-api.ts` 负责浏览器端交互请求与错误处理
- 若配置了 `API_URL`，则优先使用它
- 若未配置，浏览器端默认复用当前主机名并切换到根目录 `.env` 中的 `SERVER_PORT`
- 若未配置，服务端默认使用 `http://127.0.0.1:$SERVER_PORT`

### 后台访问控制

- `/manage` 路由由 `src/proxy.ts` 做预拦截
- 这里仅检查 `ecohub_auth_token` cookie 是否存在
- 不做角色校验，也不验证 token 真伪
- 真正的 token 校验、自动续期和写权限控制都在后端完成
- 如果前端与后端不是同源部署，需要额外确保跨域 cookie 与 CORS 配置正确

这意味着：

- 前端预拦截主要用于未登录时的快速跳转
- 后端接口才是最终权限边界
- 客户端 `axios` 拦截器会在 `401` 时跳转 `/login`，在 `403` 时提示无权限

## 常用命令

```bash
./run-web.sh

cd web
npm run build
npm run start
npm run lint
```

## 当前约束

- 管理后台依赖后端下发的 `HttpOnly` cookie 登录态
- 如果后端入口变化，需要同步更新环境变量并重新启动前端

## 相关文档

- [根目录说明](../README.md)
- [服务端说明](../server/README.md)
- [Docker 部署说明](../README-Docker.md)
- [FAQ 与排障](../README-FAQ.md)
