import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { serverGet } from "@/lib/server-api";

interface SystemRedirectPageProps {
  searchParams?: Promise<{ tab?: string }>;
}

export default async function SystemRedirectPage({ searchParams }: SystemRedirectPageProps) {
  const params = await searchParams;
  const tab = params?.tab;
  const cookieStore = await cookies();
  let isAdmin = false;
  try {
    const response = await serverGet<{ isAdmin?: boolean }>("/manage/user/info", undefined, {
      Cookie: cookieStore.toString(),
    });
    isAdmin = response.code === 0 && Boolean(response.data?.isAdmin);
  } catch {
    isAdmin = false;
  }

  if (tab === "website") {
    redirect("/manage/system/website");
  }
  if (isAdmin && (tab === "notify" || tab === "tmdb" || tab === "proxy" || tab === "security" || tab === "logs" || tab === "users")) {
    redirect(`/manage/system/${tab}`);
  }
  if (isAdmin) {
    redirect("/manage/system/proxy");
  }
  redirect("/manage/system/website");
}
