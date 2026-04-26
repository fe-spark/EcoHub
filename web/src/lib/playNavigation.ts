import { readHistoryMap, type HistoryRecord } from "@/lib/historyStorage";

type PlayEntryFallback = {
  sourceId?: string;
  episodeIndex?: number;
};

function normalizeFilmID(filmId: string | number): string {
  return String(filmId || "").trim();
}

export function buildPlayPath(
  filmId: string,
  sourceId?: string,
  episodeIndex?: number,
  currentTime?: number,
): string {
  const params = new URLSearchParams({ id: filmId });

  if (sourceId) {
    params.set("source", sourceId);
  }
  if (typeof episodeIndex === "number" && Number.isFinite(episodeIndex) && episodeIndex >= 0) {
    params.set("episode", String(episodeIndex));
  }
  if (typeof currentTime === "number" && Number.isFinite(currentTime) && currentTime > 0) {
    params.set("currentTime", String(Math.floor(currentTime)));
  }

  return `/play?${params.toString()}`;
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
  const historyRecord = resolveHistoryRecord(normalizedFilmId);

  if (historyRecord) {
    return buildPlayPath(
      normalizedFilmId,
      historyRecord.sourceId,
      historyRecord.episodeIndex,
      historyRecord.currentTime,
    );
  }

  return buildPlayPath(normalizedFilmId, fallback?.sourceId, fallback?.episodeIndex);
}
