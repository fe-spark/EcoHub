import FilmClassifySearchPageView from "./view";
import { serverGet } from "@/lib/server-api";

async function getFilmClassifySearchData(params: Record<string, string>) {
  try {
    const response = await serverGet<any>("/filmClassifySearch", params);
    if (response.code === 0) {
      return response.data;
    }
  } catch (error) {
    console.error("fetch film classify search data error:", error);
  }

  return null;
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

  const data = await getFilmClassifySearchData(currentParams);
  if (!data) {
    return null;
  }

  return <FilmClassifySearchPageView data={data} currentParams={currentParams} />;
}
