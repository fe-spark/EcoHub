import { redirect } from "next/navigation";

/** 兼容旧入口：数据重置 → 系统设置 · 数据安全 */
export default function ResetPage() {
  redirect("/manage/system?tab=security");
}
