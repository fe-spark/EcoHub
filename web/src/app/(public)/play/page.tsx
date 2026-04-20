import PlayPageView from "./view";
import { serverGet } from "@/lib/server-api";

async function getPlayData(
  filmId: string,
  sourceId?: string,
  episodeIdx?: string,
) {
  const playPageResponse = await serverGet<any>("/filmPlayInfo", {
    id: filmId,
    playFrom: sourceId,
    episode: episodeIdx || 0,
  });

  if (playPageResponse.code !== 0 || !playPageResponse.data?.detail) {
    return null;
  }

  return playPageResponse.data;
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

  let playPageData: any = null;
  try {
    playPageData = await getPlayData(filmId, sourceId, episodeIdx);
  } catch (error) {
    console.error("fetch play data error:", error);
  }

  if (!playPageData) {
    return null;
  }

  return (
    <PlayPageView
      data={playPageData}
      filmId={filmId}
      initialTime={initialTime}
    />
  );
}
