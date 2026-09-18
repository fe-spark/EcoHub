import { readHistoryMap, type HistoryRecord } from "@/lib/historyStorage";

type PlayEntryFallback = {
  sourceId?: string;
  episodeIndex?: number;
  sourceMid?: string | number;
};

function normalizeFilmID(filmId: string | number): string {
  return String(filmId || "").trim();
}

export function isLocalFilmID(filmId: string | number): boolean {
  const id = normalizeFilmID(filmId);
  if (!id || id.includes(":")) {
    return false;
  }
  const numeric = Number(id);
  return Number.isFinite(numeric) && numeric > 0;
}

export function livePlayHistoryID(sourceId?: string, sourceMid?: string | number): string {
  const source = String(sourceId || "").trim();
  const sid = String(sourceMid ?? "").trim();
  if (!source || !sid || sid === "0") {
    return "";
  }
  return `${source}:${sid}`;
}

function splitLivePlayID(filmId: string): { sourceId: string; sid: string } | null {
  const id = normalizeFilmID(filmId);
  const sep = id.lastIndexOf(":");
  if (sep <= 0 || sep === id.length - 1) {
    return null;
  }
  return { sourceId: id.slice(0, sep), sid: id.slice(sep + 1) };
}

export function buildPlayPath(
  filmId: string,
  sourceId?: string,
  episodeIndex?: number,
  currentTime?: number,
): string {
  const params = new URLSearchParams();
  params.set("id", normalizeFilmID(filmId));
  const nextSource = String(sourceId || "").trim();
  if (nextSource) {
    params.set("source", nextSource);
  }
  if (typeof episodeIndex === "number" && Number.isFinite(episodeIndex) && episodeIndex >= 0) {
    params.set("episode", String(episodeIndex));
  }
  if (typeof currentTime === "number" && Number.isFinite(currentTime) && currentTime > 0) {
    params.set("currentTime", String(Math.floor(currentTime)));
  }
  return `/play?${params.toString()}`;
}

export function buildLivePlayPath(
  sourceId?: string,
  sourceMid?: string | number,
  episodeIndex?: number,
  currentTime?: number,
): string {
  const params = new URLSearchParams();
  const source = String(sourceId || "").trim();
  const sid = String(sourceMid ?? "").trim();
  if (source) {
    params.set("source", source);
  }
  if (sid && sid !== "0") {
    params.set("sid", sid);
  }
  if (typeof episodeIndex === "number" && Number.isFinite(episodeIndex) && episodeIndex >= 0) {
    params.set("episode", String(episodeIndex));
  }
  if (typeof currentTime === "number" && Number.isFinite(currentTime) && currentTime > 0) {
    params.set("currentTime", String(Math.floor(currentTime)));
  }
  return `/play/live?${params.toString()}`;
}

function resolveHistoryRecord(filmId: string): HistoryRecord | null {
  if (!filmId) {
    return null;
  }

  const historyMap = readHistoryMap();
  const historyRecord = historyMap[filmId];

  if (!historyRecord || String(historyRecord.id) !== filmId) {
    return null;
  }

  return historyRecord;
}

export function resolvePlayEntryPath(
  filmId: string | number,
  fallback?: PlayEntryFallback,
): string {
  const normalizedFilmId = normalizeFilmID(filmId);
  const historyKey = isLocalFilmID(normalizedFilmId)
    ? normalizedFilmId
    : normalizedFilmId || livePlayHistoryID(fallback?.sourceId, fallback?.sourceMid);
  const historyRecord = resolveHistoryRecord(historyKey);

  if (historyRecord && isLocalFilmID(historyRecord.id)) {
    return buildPlayPath(
      String(historyRecord.id),
      historyRecord.sourceId,
      historyRecord.episodeIndex,
      historyRecord.currentTime,
    );
  }

  if (isLocalFilmID(normalizedFilmId)) {
    return buildPlayPath(
      normalizedFilmId,
      fallback?.sourceId,
      fallback?.episodeIndex,
    );
  }

  const live = splitLivePlayID(String(historyRecord?.id || historyKey || ""));
  return buildLivePlayPath(
    live?.sourceId || fallback?.sourceId,
    live?.sid || fallback?.sourceMid,
    historyRecord?.episodeIndex ?? fallback?.episodeIndex,
    historyRecord?.currentTime,
  );
}
