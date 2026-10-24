import CategoryPageView from "./view";
import TrackPageView from "@/components/public/TrackPageView";
import { serverGet } from "@/lib/server-api";
import { Alert } from "antd";

async function getCategoryData(pid: string) {
  try {
    const response = await serverGet<any>("/categories/overview", { pid });
    if (response.code === 0) {
      return response.data;
    }
  } catch {
    // 后端未就绪时走空态
  }

  return null;
}

export default async function CategoryPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const resolvedSearchParams = await searchParams;
  const pidValue = resolvedSearchParams.pid || resolvedSearchParams.Pid;
  const pid = Array.isArray(pidValue) ? pidValue[0] : pidValue;

  const data = await getCategoryData(pid || "");
  if (!data || !data.title || data.title.id === undefined || data.title.id === null) {
    return <Alert type="error" showIcon title="分类页面数据获取失败" />;
  }

  const effectivePid = String(data.title.id);

  return (
    <>
      <TrackPageView
        action="classify"
        resource={effectivePid}
        resourceCat={data.title.name}
        resourceTitle={data.title.name}
      />
      <CategoryPageView data={data} pid={effectivePid} />
    </>
  );
}
