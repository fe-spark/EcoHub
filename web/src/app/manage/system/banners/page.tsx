import { redirect } from "next/navigation";

/** 兼容旧入口：首页轮播已移至 内容管理 → 首页轮播 */
export default function BannersPage() {
  redirect("/manage/banners");
}
