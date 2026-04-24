import FilmClassifySearchPageView from "./view";
import { serverGet } from "@/lib/server-api";
import { Alert } from "antd";

async function getFilmClassifySearchData(params: Record<string, string>) {
  try {
    const response = await serverGet<any>("/filmClassifySearch", params);
    if (response.code === 0) {
      return { data: response.data, error: "" };
    }

    return { data: null, error: response.msg || "分类筛选数据获取失败" };
  } catch (error) {
    console.error("fetch film classify search data error:", error);
    return { data: null, error: error instanceof Error ? error.message : "分类筛选数据获取失败" };
  }
}

export default async function FilmClassifySearchPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const resolvedSearchParams = await searchParams;
  const currentParams = Object.fromEntries(
    Object.entries(resolvedSearchParams).flatMap(([key, value]) => {
      if (Array.isArray(value)) {
        return [[key, value[0] ?? ""]];
      }
      return value ? [[key, value]] : [];
    }),
  );

  const { data, error } = await getFilmClassifySearchData(currentParams);
  if (!data) {
    return <Alert type="error" showIcon message={error || "分类筛选数据获取失败"} />;
  }

  return <FilmClassifySearchPageView data={data} currentParams={currentParams} />;
}
