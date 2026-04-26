import FilmClassifyPageView from "./view";
import { serverGet } from "@/lib/server-api";
import { Alert } from "antd";

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
    return <Alert type="warning" showIcon message="缺少分类参数" />;
  }

  const data = await getFilmClassifyData(pid);
  if (!data) {
    return <Alert type="error" showIcon message="分类页面数据获取失败" />;
  }

  if (!data?.title) {
    return <Alert type="warning" showIcon message="当前分类已失效，请从最新分类导航重新进入" />;
  }

  return <FilmClassifyPageView data={data} pid={pid} />;
}
