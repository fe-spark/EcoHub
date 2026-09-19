import TrackPageView from "@/components/public/TrackPageView";
import { serverGet } from "@/lib/server-api";
import LivePlayView from "./view";

async function getLivePlayData(sourceId: string, sid: string, episodeIdx?: string) {
  const response = await serverGet<any>("/liveFilmPlayInfo", {
    source: sourceId,
    sid,
    episode: episodeIdx || 0,
  });
  if (response.code !== 0 || !response.data?.detail) {
    return null;
  }
  return response.data;
}

export default async function LivePlayPage({
  searchParams,
}: {
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}) {
  const resolved = await searchParams;
  const source = Array.isArray(resolved.source) ? resolved.source[0] : resolved.source;
  const sid = Array.isArray(resolved.sid) ? resolved.sid[0] : resolved.sid;
  const episode = Array.isArray(resolved.episode) ? resolved.episode[0] : resolved.episode;
  const initialTime = Array.isArray(resolved.currentTime)
    ? resolved.currentTime[0]
    : resolved.currentTime;

  const liveSource = String(source || "").trim();
  const liveSid = String(sid || "").trim();
  const filmId = liveSource && liveSid ? `${liveSource}:${liveSid}` : "";

  if (!liveSource || !liveSid) {
    return <LivePlayView data={null} sourceId="" sid="" emptyMessage="缺少采集源播放参数，请返回搜索后重新进入。" />;
  }

  let data: any = null;
  try {
    data = await getLivePlayData(liveSource, liveSid, episode);
  } catch {
    data = null;
  }

  if (!data) {
    return (
      <LivePlayView
        data={null}
        sourceId={liveSource}
        sid={liveSid}
        emptyMessage="当前采集源暂无播放地址，请返回搜索更换关键词或稍后再试。"
      />
    );
  }

  const detail = data.detail;
  return (
    <>
      <TrackPageView
        action="play"
        resource={filmId}
        resourceTitle={detail?.name || ""}
        resourcePoster={detail?.picture || ""}
        resourceCat={detail?.descriptor?.cName || ""}
      />
      <LivePlayView
        key={filmId}
        data={data}
        sourceId={liveSource}
        sid={liveSid}
        initialTime={initialTime}
      />
    </>
  );
}
