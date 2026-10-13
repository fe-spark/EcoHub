import CategoryFilterPageView from "./view";
import TrackPageView from "@/components/public/TrackPageView";
import { serverGet } from "@/lib/server-api";
import { Alert } from "antd";

async function getCategoryFilterData(params: Record<string, string>) {
  try {
    const response = await serverGet<any>("/films/filter", params);
    if (response.code === 0) {
      return { data: response.data, error: "" };
    }

    return { data: null, error: response.msg || "分类筛选数据获取失败" };
  } catch (error) {
    console.error("fetch category filter data error:", error);
    return {
      data: null,
      error: error instanceof Error ? error.message : "分类筛选数据获取失败",
    };
  }
}

export default async function CategoryFilterPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const resolvedSearchParams = await searchParams;
  // 过滤 Next 内部参数（_rsc 等），避免传给后端
  const currentParams = Object.fromEntries(
    Object.entries(resolvedSearchParams).flatMap(([key, value]) => {
      if (key.startsWith("_") || key.toLowerCase() === "source") {
        return [];
      }
      if (Array.isArray(value)) {
        return [[key, value[0] ?? ""]];
      }
      return value ? [[key, value]] : [];
    }),
  );

  const { data, error } = await getCategoryFilterData(currentParams);
  if (!data) {
    return (
      <Alert type="error" showIcon title={error || "分类筛选数据获取失败"} />
    );
  }

  if (!data?.title) {
    return (
      <Alert
        type="warning"
        showIcon
        title="当前分类已失效，请从最新分类导航重新进入"
      />
    );
  }

  return (
    <>
      <TrackPageView
        action="classify"
        resource={String(currentParams.pid || currentParams.Pid || "")}
        resourceCat={data.title.name}
        resourceTitle={data.title.name}
      />
      <CategoryFilterPageView data={data} currentParams={currentParams} />
    </>
  );
}
