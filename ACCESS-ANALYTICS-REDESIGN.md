# 访问分析与访问日志口径重构方案 (Access Analytics & Telemetry Redesign)

> **目标**：彻底纠偏访问统计体系，禁止各终端使用普通接口统计 PV/UV，使“访问日志”真正体现真实用户的**页面访问**与**交互埋点**，并通过 Web/App **路由拦截器**实现自动化统计。

---

## 一、 背景与现有缺陷定位

### 1. 访问日志混淆了“接口运维日志”与“业务访问日志”
- **现象**：管理员在后台【访问分析】->【访问日志】中，看到的几乎全是 `/api/index`、`/api/navCategory`、`/api/hotKeywords`、`/api/filmRelate` 等接口调用，而非用户在浏览什么页面。
- **根因**：
  - 服务端 `middleware.AccessLog()` 中间件针对每个进入 Gin 的 HTTP 请求都调用了 `access.Collect(evt)`。
  - 在 `server/internal/access/redis_store.go` 中，`writeEvent` 针对普通接口调用通过 `RecordRecent(evt)` 判定，直接将所有非 SSR 的普通 2xx 接口全量推入 Redis 的 `recent`（实时访问流水）与 `recent:<day>`。
  - 相反，真正的页面访问曝光（`evt.Method == "PAGE"`）在 `RecordRecent` 中被 `if evt.Method == "PAGE" { return false }` 显式过滤；而在 `writePageView` 中，仅有 `search`、`play`、`classify` 被推入流水，普通的页面浏览 `browse` **完全被丢弃**。
  - 结果：**访问日志变成了接口运维请求日志，用户的真实页面访问反而看不见**。

### 2. 热门榜单统计的是后端接口而非页面
- `top:path` 榜单记录的是 `GET /api/index`、`GET /api/filmRelate` 等接口路径，导致前台“热门页面/路由”榜单展示的是微服务接口，无法反映用户最常访问的实际业务页面（如 `/`、`/search`、`/play` 等）。

### 3. 各终端缺乏统一的路由拦截器机制
- **Web 端（Next.js）**：当前在部分页面的 JSX 中手动嵌入 `<TrackPageView action="browse" />`，新页面极易漏埋，且页面切路由时无法统一进行生命周期管理。
- **移动端（Android Flutter & OHOS 鸿蒙）**：在各个 Page 的 `initState` 或 `aboutToAppear` 生命周期中硬编码调用 `trackView`，分散且缺乏统一的路由拦截层。

---

## 二、 核心统计口径与设计原则

```mermaid
flowchart TD
    subgraph 客户端 (Web / App)
        R[路由拦截器 Navigation Interceptor] -->|页面切换自动触发| P[页面访问事件 PageView]
        U[用户主动交互 Click / Play / Search] -->|交互回调显式触发| A[用户操作埋点 UserAction]
    end

    subgraph 后端接口调用 (Internal API)
        API[普通数据接口 /api/index, /api/navCategory 等]
    end

    subgraph 数据分流与统计处理
        P -->|POST /api/stat/view| S1[PV/UV 核心统计 + 访问日志流水 + 热门页面榜单]
        A -->|POST /api/stat/view| S2[行为统计 + 实时操作流水 + 热搜/热播榜单]
        API -->|AccessLog 中间件| M[服务质量监控: 仅保留慢请求 slow / 异常请求 error / 耗时直方图]
    end
```

### 1. 严格划清界限
| 维度 | 页面访问 (PageView) | 用户交互埋点 (UserAction) | 后端接口请求 (HTTP API) |
| :--- | :--- | :--- | :--- |
| **触发来源** | 客户端全局路由拦截器 | 用户点击/搜索/播放等业务动作 | 前端/客户端底层 `fetch`/`http` 数据请求 |
| **统计指标** | **计入 PV、UV、热门页面榜单** | 计入对应业务 Action 量、热播/热搜榜单 | **绝不计入 PV/UV，绝不计入热门页面** |
| **访问日志** | **推入实时访问日志（作为页面访问记录）** | **推入实时访问日志（作为操作行为记录）** | **绝不推入 `recent` 全部访问日志** |
| **日志归宿** | 访问分析 -> 访问日志【全部】 | 访问分析 -> 访问日志【全部】 | 仅在状态码 $\ge 400$（异常）或耗时 $\ge 500\text{ms}$（慢请求）时采样存入系统监控 |

### 2. PV / UV 口径说明
- **PV (Page View)**：仅且只能由客户端路由拦截器上报的**页面访问事件**累计产生。一次页面刷新或一次路由切换计为 1 次 PV。
- **UV (Unique Visitor)**：在有效时间窗口（按自然日）内，基于真实访客的去重哈希（IP / 设备标识）通过 HyperLogLog 统计，体现真实访问网站或应用的人数。

---

## 三、 详细改造方案

### 1. 服务端重构 (`server/internal/access/`)

#### 1.1 `redis_store.go`：隔离业务访问日志与接口性能监控
- **彻底关闭普通接口进入 `recent` 流水**：
  - 修改 `writeEvent`：移除非 TVBox 接口向 `recentKey` / `recentDayKey` 的推送。
  - 普通接口仅保留：
    - `slowKey` / `slowDayKey`（耗时 $\ge \text{AccessSlowMs}$ 慢请求排障）
    - `errorKey` / `errorDayKey`（状态码 $\ge 400$ 异常接口排障）
    - `histKey`（延迟分桶与 P95 统计）
- **`writePageView` 成为业务访问日志唯一入口**：
  - **所有** 页面访问（`action == "browse"` 或其他页面级动作）以及用户行为埋点（`search`、`play`、`classify`），全部格式化推入 `recentKey` 和 `recentDayKey`。
  - 在 `writePageView` 中维护 `topPathKey`：记录用户访问的真实前端页面路径（如 `/`、`/search`、`/play` 等），生成真实的热门页面榜单。

#### 1.2 `event.go` & `classify.go`：重构过滤规则
- `RecordRecent` 明确废除普通 2xx 接口采样，仅用于系统级诊断。
- 移除管理后台路径 `/api/manage/*` 对业务统计的干扰。

#### 1.3 `page.go`：规范埋点协议
- 请求体扩展支持：
  ```json
  {
    "action": "browse",          // 事件类型: browse(页面浏览), play(点播), search(搜索), classify(筛选)
    "path": "/play?id=1024",     // 页面路由路径 (带查询参数)
    "resource": "1024",          // 关联资源 (影视ID / 搜索词 / 分类ID)
    "source": "web"              // 终端类型: web / app / android / ohos / ios
  }
  ```
- 放宽 `pageActions` 限制，支持通用页面路由与扩展行为上报。

---

### 2. Web 前端重构 (`web/`)

#### 2.1 全局路由拦截器 (`RouteTracker.tsx`)
在 `web/src/components/public/RouteTracker.tsx` 中基于 Next.js App Router 统一监听客户端路由跳转：
```tsx
"use client";

import { useEffect, useRef } from "react";
import { usePathname, useSearchParams } from "next/navigation";
import { trackPageView } from "@/lib/track-page-view";

export default function RouteTracker() {
  const pathname = usePathname();
  const searchParams = useSearchParams();
  const lastUrlRef = useRef("");

  useEffect(() => {
    // 排除后台管理等无需统计前台 PV 的路由
    if (!pathname || pathname.startsWith("/manage") || pathname.startsWith("/api")) {
      return;
    }

    const queryString = searchParams?.toString();
    const currentFullUrl = queryString ? `${pathname}?${queryString}` : pathname;

    if (currentFullUrl === lastUrlRef.current) {
      return;
    }
    lastUrlRef.current = currentFullUrl;

    // 路由拦截器自动触发页面访问上报
    trackPageView({
      action: "browse",
      path: currentFullUrl,
      source: "web",
    });
  }, [pathname, searchParams]);

  return null;
}
```
在 `web/src/app/(public)/layout.tsx` 中引入 `<RouteTracker />`，**实现全站无侵入、全自动页面 PV/UV 统计**。

#### 2.2 行为埋点 SDK 规范化 (`track-page-view.ts`)
提供专门的交互埋点方法：
- `trackPageNavigation(path)`：供路由拦截器使用。
- `trackPlayAction(filmId)`：在用户实际点击播放/解析成功时触发。
- `trackSearchAction(keyword)`：在用户提交搜索词时触发。
- `trackClassifyAction(categoryId)`：在用户切换分类标签时触发。

#### 2.3 清理业务页面
移除各业务页面（`page.tsx`、`search/page.tsx`、`play/page.tsx`、`filmClassify/page.tsx`）中原先零散挂载的 `<TrackPageView />`。

---

### 3. 移动端客户端改造规范 (App Client)

#### 3.1 Android Flutter (`app-for-android`)
- **路由拦截器**：
  利用 Flutter 的 `NavigatorObserver` 捕获路由跳转：
  ```dart
  class EcoHubRouteObserver extends NavigatorObserver {
    @override
    void didPush(Route<dynamic> route, Route<dynamic>? previousRoute) {
      super.didPush(route, previousRoute);
      _reportRoute(route);
    }

    @override
    void didReplace({Route<dynamic>? newRoute, Route<dynamic>? oldRoute}) {
      super.didReplace(newRoute: newRoute, oldRoute: oldRoute);
      if (newRoute != null) _reportRoute(newRoute);
    }

    void _reportRoute(Route<dynamic> route) {
      final name = route.settings.name;
      if (name != null && name.isNotEmpty) {
        HttpClient.instance.trackPageView(path: name, source: 'android');
      }
    }
  }
  ```
  在 `MaterialApp` 中配置 `navigatorObservers: [EcoHubRouteObserver()]`。
- **用户交互埋点**：
  在点播播放器启动、搜索按钮点击等业务交互点保留显式的 `trackAction` 埋点调用。

#### 3.2 鸿蒙 OHOS (`app-for-ohos`)
- 在 Navigation / 路由中心通过全局导航监听器统一拦截页面切换并调用 `/stat/view` 上报；业务交互（播放、搜索）单独调用埋点函数。

---

### 4. 管理后台展示优化 (`web/src/app/manage/access/view/`)

- **访问日志表格字段升级**：
  - **请求路径**：直接展示前端页面路由（如 `/`、`/play?id=1024`、`/search?keyword=阿凡达`），并可附带页面中文含义标签。
  - **业务场景**：
    - `[页面访问]`：首页浏览、播放页访问、搜索页访问、分类页访问
    - `[用户操作]`：影视点播、寻片搜索、标签筛选
  - 彻底移除对 `/api/index`、`/api/navCategory`、`/api/filmRelate`、`/api/hotKeywords` 等底层接口的人工包装代码。
- **分栏清晰**：
  - 【全部】：仅展示真实用户的页面访问轨迹与行为流水。
  - 【慢请求】：系统运维指标，展示耗时较长的接口。
  - 【异常】：系统运维指标，展示 4xx/5xx 的故障接口。

---

## 四、 改造效果对比

| 场景 | 改造前（现有现状） | 改造后（本次目标） |
| :--- | :--- | :--- |
| **用户访问首页** | 触发 1 次 `browse`（被日志丢弃）；同时触发 4~5 个 `/api/*` 接口，**访问日志被 5 条接口刷屏** | 路由拦截器上报 1 条 `GET /` 页面访问，**访问日志清晰显示 1 条“首页浏览 [/]”** |
| **用户播放电影** | 访问日志显示 `GET /api/filmPlayInfo?id=1024` | 访问日志显示页面访问 `/play?id=1024` 与用户操作 `影视点播 [影片 #1024]` |
| **热门路由榜单** | 榜首是 `/api/index`、`/api/navCategory` | 榜首是前端页面 `/`、`/search`、`/play` 等真实页面 |
| **PV / UV 指标** | 口径混乱，受前端发起的请求数影响 | **纯净的页面访问次数 (PV) 与独立访客数 (UV)** |
