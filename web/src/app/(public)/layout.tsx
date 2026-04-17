import PublicLayoutView from "./layout-view";
import { serverGet } from "@/lib/server-api";

interface NavItem {
  id: string;
  name: string;
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
  const navList = await getNavList();

  return <PublicLayoutView navList={navList}>{children}</PublicLayoutView>;
}
