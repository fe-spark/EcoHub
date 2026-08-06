import { PublicContentLoadingPanel } from "@/components/public/PublicContentLoading";

/**
 * 路由段 loading：作为 publicMain 的 flex 子项撑满 Header～Footer 中间。
 * 顶栏进度仍由 Header / startNavigationLoading 管理。
 */
export default function PublicRouteLoading() {
  return <PublicContentLoadingPanel label="页面加载中..." fill />;
}
