import { cookies } from "next/headers";
import PublicLayoutView from "./layout-view";
import {
  isServerUnauthorizedError,
  redirectToLogin,
  serverGet,
} from "@/lib/server-api";
import type { SiteConfig } from "@/components/common/SiteGuard";

interface NavItem {
  id: string;
  name: string;
}

async function isSitePrivateAccess(): Promise<boolean> {
  try {
    const resp = await serverGet<SiteConfig>("/config/basic");
    return resp.code === 0 && Boolean(resp.data?.privateAccess);
  } catch {
    return false;
  }
}

async function verifyPublicAccess(): Promise<{ hasToken: boolean; confirmedPublic: boolean }> {
  const cookieStore = await cookies();
  const authToken = cookieStore.get("ecohub_auth_token")?.value?.trim();
  if (authToken) {
    // 令牌是否有效由下方受 PrivateAccessGuard 保护的 /categories 请求一并校验，避免额外请求
    return { hasToken: true, confirmedPublic: false };
  }

  const isPrivate = await isSitePrivateAccess();
  if (isPrivate) {
    await redirectToLogin();
  }
  return { hasToken: false, confirmedPublic: !isPrivate };
}

async function getNavList(): Promise<{ items: NavItem[]; unauthorized: boolean; failed: boolean }> {
  try {
    const response = await serverGet<NavItem[]>("/categories");
    if (response.code === 0 && Array.isArray(response.data)) {
      return { items: response.data, unauthorized: false, failed: false };
    }
    // 收到 HTTP 200 说明后端已通过私有化鉴权（例如空库暂无分类），不再误判为未鉴权触发登录死循环
    return { items: [], unauthorized: false, failed: false };
  } catch (error) {
    if (isServerUnauthorizedError(error)) {
      return { items: [], unauthorized: true, failed: true };
    }
    // 非 401 异常（如 5xx/网络中断）先标记 failed，结合私有化状态决定是否拦截
    return { items: [], unauthorized: false, failed: true };
  }
}

export default async function PublicLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  const { hasToken, confirmedPublic } = await verifyPublicAccess();

  const navResult = await getNavList();
  if (navResult.unauthorized) {
    await redirectToLogin();
  }
  // 若携带了 Cookie 但 /categories 因非 401 错误未通过，在私有化模式下禁止放行空壳渲染
  if (navResult.failed && hasToken && !confirmedPublic) {
    if (await isSitePrivateAccess()) {
      await redirectToLogin();
    }
  }

  return <PublicLayoutView navList={navResult.items}>{children}</PublicLayoutView>;
}
