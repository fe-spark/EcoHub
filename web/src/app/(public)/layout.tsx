import { cookies, headers } from "next/headers";
import { redirect } from "next/navigation";
import PublicLayoutView from "./layout-view";
import { serverGet } from "@/lib/server-api";
import type { SiteConfig } from "@/components/common/SiteGuard";

interface NavItem {
  id: string;
  name: string;
}

async function verifyPublicAccess() {
  const cookieStore = await cookies();
  const hasAuth = cookieStore.has("ecohub_auth_token");
  if (hasAuth) return;

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
    const headerStore = await headers();
    const currentPath = headerStore.get("x-current-path");
    if (currentPath && currentPath.startsWith("/") && !currentPath.startsWith("//")) {
      redirect(`/login?redirect=${encodeURIComponent(currentPath)}`);
    } else {
      redirect("/login");
    }
  }
}

async function getNavList(): Promise<NavItem[]> {
  try {
    const response = await serverGet<NavItem[]>("/navCategory");
    if (response.code === 0 && Array.isArray(response.data)) {
      return response.data;
    }
  } catch (error) {
    console.error("fetch nav category error:", error);
  }

  return [];
}

export default async function PublicLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  await verifyPublicAccess();

  const navList = await getNavList();

  return <PublicLayoutView navList={navList}>{children}</PublicLayoutView>;
}
