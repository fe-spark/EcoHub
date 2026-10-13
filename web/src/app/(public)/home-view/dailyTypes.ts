export const REFRESH_MS = 15 * 1000;
export const PAGE_SIZE = 6;

export interface DailyFilm {
  id: string;
  mid?: string;
  name: string;
  picture: string;
  year: string;
  cName: string;
  area: string;
  language?: string;
  classTag?: string;
  remarks: string;
  blurb?: string;
}

export function filmId(item: DailyFilm) {
  return String(item.id ?? item.mid ?? "").trim();
}

export function filmTitle(name?: string) {
  return (name ?? "").split("[")[0].trim();
}

export function normalizeMeta(value?: string | number | null) {
  const text = String(value ?? "").trim();
  if (!text || text === "0") {
    return "";
  }
  return text;
}

export function filmTags(item: DailyFilm) {
  const category = normalizeMeta(item.cName);
  const year = normalizeMeta(item.year ? String(item.year).slice(0, 4) : "");
  const plotTag = normalizeMeta(item.classTag?.split(/[,，/|、\s]+/)[0]);
  const area = normalizeMeta(item.area?.split(",")[0]);
  const tags: string[] = [];
  if (category) tags.push(category);
  if (year) tags.push(year);
  if (plotTag && !tags.some((t) => t.includes(plotTag) || plotTag.includes(t))) tags.push(plotTag);
  if (area && !tags.some((t) => t.includes(area) || area.includes(t))) tags.push(area);
  return tags.filter(Boolean);
}
