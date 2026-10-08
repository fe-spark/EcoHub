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
