import { redirect } from "next/navigation";

export default function ProxyRedirectPage() {
  redirect("/manage/system/proxy");
}
