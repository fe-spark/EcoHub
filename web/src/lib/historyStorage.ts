export type HistoryRecord = {
  id: number | string;
  name: string;
  picture?: string;
  sourceId?: string;
  episodeIndex?: number;
  sourceName?: string;
  episode?: string;
  timeStamp: number;
  link: string;
  currentTime?: number;
  duration?: number;
  devices?: boolean;
};

type HistoryMap = Record<string, HistoryRecord>;

const HISTORY_KEY = "filmHistory";
const MAX_HISTORY_ITEMS = 200;

function safeParseHistory(raw: string | null | undefined): HistoryMap {
  if (!raw) return {};

  try {
    const parsed = JSON.parse(raw);
    return parsed && typeof parsed === "object" ? parsed : {};
  } catch {
    return {};
  }
}

function trimHistoryMap(historyMap: HistoryMap): HistoryMap {
  const trimmedEntries = Object.entries(historyMap)
    .sort(([, a], [, b]) => (b.timeStamp || 0) - (a.timeStamp || 0))
    .slice(0, MAX_HISTORY_ITEMS);

  return Object.fromEntries(trimmedEntries);
}

export function readHistoryMap(): HistoryMap {
  if (typeof window === "undefined") return {};

  const localRaw = window.localStorage.getItem(HISTORY_KEY);
  return safeParseHistory(localRaw);
}

export function writeHistoryMap(historyMap: HistoryMap) {
  if (typeof window === "undefined") return;

  const trimmedHistoryMap = trimHistoryMap(historyMap);
  window.localStorage.setItem(HISTORY_KEY, JSON.stringify(trimmedHistoryMap));
}

export function clearHistoryMap() {
  if (typeof window === "undefined") return;

  window.localStorage.removeItem(HISTORY_KEY);
}
