import { redirect } from "next/navigation";

interface SystemRedirectPageProps {
  searchParams?: Promise<{ tab?: string }>;
}

export default async function SystemRedirectPage({ searchParams }: SystemRedirectPageProps) {
  const params = await searchParams;
  const tab = params?.tab;
  if (tab === "notify" || tab === "tmdb" || tab === "proxy" || tab === "security" || tab === "logs" || tab === "website") {
    redirect(`/manage/system/${tab}`);
  }
  redirect("/manage/system/proxy");
}
