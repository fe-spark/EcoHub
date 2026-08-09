import { redirect } from "next/navigation";

/** 兼容旧入口：首页封面 → 系统设置 · 网站配置（同页含封面） */
export default function BannersPage() {
  redirect("/manage/system?tab=website");
}
