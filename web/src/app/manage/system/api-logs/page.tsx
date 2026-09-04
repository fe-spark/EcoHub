import { redirect } from "next/navigation";

export default function SystemApiLogsPage() {
  redirect("/manage/system?tab=api-logs");
}
