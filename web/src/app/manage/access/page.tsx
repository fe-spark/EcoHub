import { cookies } from "next/headers";
import { redirect } from "next/navigation";
import { serverGet } from "@/lib/server-api";
import AccessPageView from "./view";

export default async function AccessPage() {
  const cookieStore = await cookies();
  try {
    const response = await serverGet<{ id?: number }>("/manage/user/info", undefined, {
      Cookie: cookieStore.toString(),
    });
    if (response.code !== 0 || !response.data?.id) {
      redirect("/login");
    }
  } catch {
    redirect("/login");
  }
  return <AccessPageView />;
}
