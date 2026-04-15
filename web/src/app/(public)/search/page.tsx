import SearchPageClient from "../SearchPageClient";
import { serverGet } from "@/lib/server-api";

async function getSearchData(keyword: string, current: string) {
  if (!keyword) {
    return null;
  }

  try {
    const response = await serverGet<any>("/searchFilm", {
      keyword,
      current,
    });

    if (response.code === 0) {
      return response.data;
    }
  } catch (error) {
    console.error("fetch search data error:", error);
  }

  return null;
}

export default async function SearchPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const resolvedSearchParams = await searchParams;
  const search = resolvedSearchParams.search;
  const current = resolvedSearchParams.current;
  const keyword = Array.isArray(search) ? search[0] : (search ?? "");
  const currentPage = Array.isArray(current) ? current[0] : (current ?? "1");
  const data = await getSearchData(keyword, currentPage);

  return <SearchPageClient data={data} keyword={keyword} current={currentPage} />;
}
