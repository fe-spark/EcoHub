import FilmClassifyClient from "../FilmClassifyClient";
import { serverGet } from "@/lib/server-api";

async function getFilmClassifyData(pid: string) {
  try {
    const response = await serverGet<any>("/filmClassify", { Pid: pid });
    if (response.code === 0) {
      return response.data;
    }
  } catch (error) {
    console.error("fetch film classify data error:", error);
  }

  return null;
}

export default async function FilmClassifyPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const resolvedSearchParams = await searchParams;
  const pidValue = resolvedSearchParams.Pid;
  const pid = Array.isArray(pidValue) ? pidValue[0] : pidValue;

  if (!pid) {
    return null;
  }

  const data = await getFilmClassifyData(pid);
  if (!data) {
    return null;
  }

  return <FilmClassifyClient data={data} pid={pid} />;
}
