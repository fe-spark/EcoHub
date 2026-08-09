import { redirect } from "next/navigation";

export default function SystemLogsPage() {
  redirect("/manage/system?tab=logs");
}



