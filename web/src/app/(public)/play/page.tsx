import PlayPageView from "./view";
import { serverGet } from "@/lib/server-api";

async function getPlayData(
  filmId: string,
  sourceId?: string,
  episodeIdx?: string,
) {
  const response = await serverGet<any>("/filmPlayInfo", {
    id: filmId,
    playFrom: sourceId,
    episode: episodeIdx || 0,
  });

  if (response.code !== 0 || !response.data?.detail) {
    return null;
  }

  return response.data;
}

export default async function PlayPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const resolvedSearchParams = await searchParams;
  const idValue = resolvedSearchParams.id;
  const sourceValue = resolvedSearchParams.source;
  const episodeValue = resolvedSearchParams.episode;
  const currentTimeValue = resolvedSearchParams.currentTime;

  const filmId = Array.isArray(idValue) ? idValue[0] : idValue;
  const sourceId = Array.isArray(sourceValue) ? sourceValue[0] : sourceValue;
  const episodeIdx = Array.isArray(episodeValue) ? episodeValue[0] : episodeValue;
  const initialTime = Array.isArray(currentTimeValue)
    ? currentTimeValue[0]
    : currentTimeValue;

  if (!filmId) {
    return null;
  }

  let data: any = null;
  try {
    data = await getPlayData(filmId, sourceId, episodeIdx);
  } catch (error) {
    console.error("fetch play data error:", error);
  }

  if (!data) {
    return null;
  }

  return (
    <PlayPageView
      data={data}
      filmId={filmId}
      initialTime={initialTime}
    />
  );
}
