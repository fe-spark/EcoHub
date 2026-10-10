import SearchPageView from "./view";
import TrackPageView from "@/components/public/TrackPageView";
import { serverGet } from "@/lib/server-api";

async function getSearchData(keyword: string, sort?: string) {
  if (!keyword) {
    return null;
  }

  try {
    const response = await serverGet<any>("/searchFilm", {
      keyword,
      current: "1",
      pageSize: 12,
      sort: sort || "",
    });

    if (response.code === 0) {
      return response.data;
    }
  } catch (error) {
    console.error("fetch search data error:", error);
  }

  return null;
}

async function getHotKeywordsData(): Promise<string[]> {
  try {
    const response = await serverGet<string[]>("/hotKeywords", { limit: 8 });
    if (response.code === 0 && Array.isArray(response.data)) {
      return response.data;
    }
  } catch (error) {
    console.error("fetch hot keywords error:", error);
  }
  return [];
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

  const [data, hotKeywords] = await Promise.all([
    getSearchData(keyword, currentSort),
    getHotKeywordsData(),
  ]);

  return (
    <>
      <TrackPageView action={keyword.trim() ? "search" : "browse"} resource={keyword} />
      <SearchPageView
        data={data}
        keyword={keyword}
        sort={currentSort}
        hotKeywords={hotKeywords}
      />
    </>
  );
}
