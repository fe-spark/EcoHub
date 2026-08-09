import { redirect } from "next/navigation";

/** 兼容旧入口：通知设置 → 系统设置 · 通知配置 */
export default function NotifyPage() {
  redirect("/manage/system?tab=notify");
}
