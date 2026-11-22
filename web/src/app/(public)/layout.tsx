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

async function verifyPublicAccess() {
  const cookieStore = await cookies();
  const authToken = cookieStore.get("ecohub_auth_token")?.value?.trim();
  if (authToken) {
    // 令牌是否有效由下方受 PrivateAccessGuard 保护的 /categories 请求一并校验，避免额外请求
    return;
  }

  let isPrivate = false;
  try {
    const resp = await serverGet<SiteConfig>("/config/basic");
    if (resp.code === 0 && resp.data?.privateAccess) {
      isPrivate = true;
    }
  } catch {
    // 忽略请求异常
  }

  if (isPrivate) {
    await redirectToLogin();
  }
}

async function getNavList(): Promise<{ items: NavItem[]; unauthorized: boolean }> {
  try {
    const response = await serverGet<NavItem[]>("/categories");
    if (response.code === 0 && Array.isArray(response.data)) {
      return { items: response.data, unauthorized: false };
    }
  } catch (error) {
    if (isServerUnauthorizedError(error)) {
      return { items: [], unauthorized: true };
    }
    // 导航失败时用空列表，避免把可恢复错误打到客户端 overlay
  }

  return { items: [], unauthorized: false };
}

export default async function PublicLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  await verifyPublicAccess();

  const navResult = await getNavList();
  if (navResult.unauthorized) {
    await redirectToLogin();
  }

  return <PublicLayoutView navList={navResult.items}>{children}</PublicLayoutView>;
}
