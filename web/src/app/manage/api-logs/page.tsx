import { redirect } from "next/navigation";

export default function ApiLogsPage() {
  redirect("/manage/system?tab=api-logs");
}

