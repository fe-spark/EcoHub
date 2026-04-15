import FilmDetailClient from "../FilmDetailClient";
import { serverGet } from "@/lib/server-api";

function normalizeDetailContent(content: string) {
  return content
    .replace(/<br\s*\/?>/gi, "\n")
    .replace(/<\/p>/gi, "\n")
    .replace(/(&.*;)|( )|(　　)|(<[^>]+>)/g, "")
    .replace(/\n+/g, "\n")
    .trim();
}

async function getFilmDetail(link: string) {
  const response = await serverGet<any>("/filmDetail", { id: link });
  if (response.code !== 0 || !response.data?.detail) {
    return null;
  }

  const detail = response.data.detail;
  detail.name = detail.name.replace(/(～.*～)/g, "");
  if (detail.descriptor?.content) {
    detail.descriptor.content = normalizeDetailContent(detail.descriptor.content);
  }

  return response.data;
}

export default async function FilmDetailPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const resolvedSearchParams = await searchParams;
  const link = resolvedSearchParams.link;
  const filmLink = Array.isArray(link) ? link[0] : link;

  if (!filmLink) {
    return null;
  }

  let data: any = null;
  try {
    data = await getFilmDetail(filmLink);
  } catch (error) {
    console.error("fetch film detail error:", error);
  }

  if (!data) {
    return null;
  }

  return <FilmDetailClient data={data} link={filmLink} />;
}
