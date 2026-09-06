import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { serverGet } from "@/lib/server-api";
import AccessPageView from "./view";

export default async function AccessPage() {
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
  if (!isAdmin) {
    redirect("/manage");
  }
  try {
    const statusResp = await serverGet<{ enabled?: boolean }>("/manage/access/status", undefined, {
      Cookie: cookieStore.toString(),
    });
    if (statusResp.code !== 0 || !statusResp.data?.enabled) {
      redirect("/manage");
    }
  } catch {
    redirect("/manage");
  }
  return <AccessPageView />;
}
