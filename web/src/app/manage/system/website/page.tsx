import { redirect } from "next/navigation";

/** 兼容旧入口：网站配置 → 系统设置 · 网站配置 */
export default function SiteConfigPage() {
  redirect("/manage/system?tab=website");
}
