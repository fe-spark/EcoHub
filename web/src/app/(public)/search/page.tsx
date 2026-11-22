import { Alert } from "antd";
import SearchPageView from "./view";
import TrackPageView from "@/components/public/TrackPageView";
import {
  getPublicServerErrorMessage,
  isServerUnauthorizedError,
  redirectToLogin,
  serverGet,
} from "@/lib/server-api";

async function getSearchData(keyword: string, sort?: string) {
  if (!keyword) {
    return { data: null, error: "", unauthorized: false };
  }

  try {
    const response = await serverGet<any>("/films/search", {
      keyword,
      current: "1",
      pageSize: 12,
      sort: sort || "",
    });

    if (response.code === 0) {
      return { data: response.data, error: "", unauthorized: false };
    }

    return {
      data: null,
      error: response.msg || "搜索服务暂不可用",
      unauthorized: false,
    };
  } catch (error) {
    if (isServerUnauthorizedError(error)) {
      return { data: null, error: "", unauthorized: true };
    }
    return {
      data: null,
      error: getPublicServerErrorMessage(error),
      unauthorized: false,
    };
  }
}

async function getHotKeywordsData(): Promise<{
  keywords: string[];
  unauthorized: boolean;
}> {
  try {
    const response = await serverGet<string[]>("/search/hot-keywords", {
      limit: 8,
    });
    if (response.code === 0 && Array.isArray(response.data)) {
      return { keywords: response.data, unauthorized: false };
    }
  } catch (error) {
    if (isServerUnauthorizedError(error)) {
      return { keywords: [], unauthorized: true };
    }
    // 热词失败时用空列表
  }
  return { keywords: [], unauthorized: false };
}

export default async function SearchPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const resolvedSearchParams = await searchParams;
  const search = resolvedSearchParams.search;
  const sortParam = resolvedSearchParams.sort;
  const keyword = Array.isArray(search) ? search[0] : (search ?? "");
  const currentSort = Array.isArray(sortParam) ? sortParam[0] : (sortParam ?? "");

  const [searchResult, hotResult] = await Promise.all([
    getSearchData(keyword, currentSort),
    getHotKeywordsData(),
  ]);

  if (searchResult.unauthorized || hotResult.unauthorized) {
    await redirectToLogin();
  }

  if (searchResult.error) {
    return (
      <Alert
        type="error"
        showIcon
        title="搜索服务暂时不可用"
        description={searchResult.error}
      />
    );
  }

  return (
    <>
      <TrackPageView
        action={keyword.trim() ? "search" : "browse"}
        resource={keyword}
      />
      <SearchPageView
        data={searchResult.data}
        keyword={keyword}
        sort={currentSort}
        hotKeywords={hotResult.keywords}
      />
    </>
  );
}
