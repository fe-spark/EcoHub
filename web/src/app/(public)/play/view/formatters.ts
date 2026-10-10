import { buildPlayPath } from "@/lib/playNavigation";

export function formatLocalUpdateTime(value?: string | number | null): string {
  const stamp = Number(value);
  if (!Number.isFinite(stamp) || stamp <= 0) return "";

  const date = new Date(stamp * 1000);
  if (Number.isNaN(date.getTime())) return "";

  const pad = (part: number) => String(part).padStart(2, "0");
  return `${date.getFullYear()}-${pad(date.getMonth() + 1)}-${pad(date.getDate())} ${pad(date.getHours())}:${pad(date.getMinutes())}`;
}

export function formatActorNames(value?: string): string {
  const raw = String(value || "").trim();
  if (!raw) return "暂无";
  return raw.replace(/\s*[，,、]\s*/g, " / ");
}

export function resolveFilmScore(descriptor?: { score?: string; dbScore?: string }): string {
  return String(descriptor?.score || descriptor?.dbScore || "").trim() || "9.0";
}

export function parseInitialTimeParam(value?: string): number {
  if (!value) return 0;
  const parsed = Number.parseFloat(value);
  return Number.isFinite(parsed) && parsed > 0 ? parsed : 0;
}

export function makeEpisodeKey(sourceId: string, episodeIndex: number) {
  return `${sourceId}:${episodeIndex}`;
}

export function buildPlayLink(
  filmId: string | number,
  sourceId: string,
  episodeIndex: number,
  currentTime = 0,
) {
  return buildPlayPath(String(filmId), sourceId, episodeIndex, currentTime);
}

export function buildInitialPlaybackState(data: any, initialTime?: string) {
  const playingSourceId = data?.currentPlayFrom || "";
  const episodeIndex = data?.currentEpisode ?? 0;

  return {
    playingSourceId,
    viewingSourceId: playingSourceId,
    current: data?.current ? { index: episodeIndex, ...data.current } : null,
    playInitialTime: parseInitialTimeParam(initialTime),
  };
}
