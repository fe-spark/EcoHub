import { redirect } from "next/navigation";
import PlayPageView from "./view";
import TrackPageView from "@/components/public/TrackPageView";
import { buildLivePlayPath } from "@/lib/playNavigation";
import { serverGet } from "@/lib/server-api";

function firstParam(value: string | string[] | undefined) {
  return Array.isArray(value) ? value[0] : value;
}

async function getPlayData(filmId: string, sourceId?: string, episodeIdx?: string) {
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
  const resolved = await searchParams;
  const filmId = String(firstParam(resolved.id) || "").trim();
  const sourceId = firstParam(resolved.source);
  const episodeIdx = firstParam(resolved.episode);
  const initialTime = firstParam(resolved.currentTime);
  const sid = String(firstParam(resolved.sid) || "").trim();
  const numericId = Number(filmId);

  if (!(Number.isFinite(numericId) && numericId > 0)) {
    const liveSource = String(sourceId || "").trim();
    if (liveSource && sid) {
      redirect(buildLivePlayPath(liveSource, sid, episodeIdx ? Number(episodeIdx) : 0));
    }
    return <PlayPageView data={null} filmId="" emptyMessage="未找到影片参数，请返回列表重新进入播放页。" />;
  }

  let playPageData: any = null;
  try {
    playPageData = await getPlayData(filmId, sourceId, episodeIdx);
  } catch (error) {
    console.error("fetch play data error:", error);
  }

  if (!playPageData) {
    return (
      <PlayPageView
        data={null}
        filmId={filmId}
        emptyMessage="当前影片播放数据不存在或已失效，请切换片源或返回详情页重试。"
      />
    );
  }

  const filmDetail = playPageData?.detail;
  const filmPoster =
    filmDetail?.isCustomPicture && filmDetail?.customPicture
      ? filmDetail.customPicture
      : filmDetail?.picture || "";

  return (
    <>
      <TrackPageView
        action="play"
        resource={filmId}
        resourceTitle={filmDetail?.name || ""}
        resourcePoster={filmPoster}
        resourceCat={filmDetail?.descriptor?.cName || ""}
      />
      <PlayPageView key={filmId} data={playPageData} filmId={filmId} initialTime={initialTime} />
    </>
  );
}
