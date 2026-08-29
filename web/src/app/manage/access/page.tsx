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
  return <AccessPageView />;
}
