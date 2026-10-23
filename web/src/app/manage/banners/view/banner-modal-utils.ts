import { ApiGet, ApiPost } from "@/lib/client-api";
import {
  BannerRecord,
  BannerFormValues,
  FilmOption,
  EditorMode,
  resolvePreviewPicture,
  resolveEditablePicture,
} from "./types";

export async function uploadBannerFile(file: File): Promise<string> {
  const formData = new FormData();
  formData.append("file", file);
  const resp = await ApiPost("/manage/file/upload", formData);
  if (resp?.code === 0 && resp.data) {
    return typeof window !== "undefined" && String(resp.data).startsWith("/")
      ? `${window.location.origin}${resp.data}`
      : String(resp.data);
  }
  throw new Error(resp?.msg || "上传失败");
}

export function initBannerFormValues(
  mode: EditorMode,
  currentRow: BannerRecord | null,
): Partial<BannerFormValues> {
  if (mode === "edit" && currentRow) {
    const isCustom = currentRow.isCustomPic === true || Boolean(currentRow.customPicture?.trim());
    const rawSlide = (currentRow.pictureSlide || "").trim();
    const rawPic = (currentRow.picture || "").trim();
    const rawPoster = (currentRow.poster || "").trim();
    const validSlide = (rawSlide && rawSlide !== rawPic && rawSlide !== rawPoster) ? rawSlide : "";
    return {
      mid: currentRow.mid,
      isCustomPic: isCustom,
      picture: isCustom ? resolveEditablePicture(currentRow) : "",
      pictureSlide: isCustom ? validSlide : "",
    };
  }
  return { isCustomPic: false };
}

interface BuildPayloadParams {
  values: BannerFormValues;
  currentRow: BannerRecord | null;
  selectedFilm: FilmOption | null;
  isReplacingFilm: boolean;
  scrapedData?: {
    score?: number;
    blurb?: string;
    tmdbId?: number;
  };
}

export function buildBannerPayload({
  values,
  currentRow,
  selectedFilm,
  isReplacingFilm,
  scrapedData,
}: BuildPayloadParams): BannerRecord {
  const isCustom = Boolean(values.isCustomPic);
  const customPic = isCustom ? (values.picture || "").trim() : "";
  const customSlide = isCustom ? (values.pictureSlide || "").trim() : "";
  const baseFilm = isReplacingFilm ? selectedFilm : (selectedFilm || currentRow);
  const livePic = baseFilm?.picture || "";
  const rawLiveSlide = baseFilm?.pictureSlide?.trim() || "";
  const liveSlide = (rawLiveSlide && rawLiveSlide !== livePic) ? rawLiveSlide : "";
  const finalSlide = isCustom ? customSlide : liveSlide;

  const rawYear = baseFilm?.year;
  const parsedYear = typeof rawYear === "number" ? rawYear : parseInt(String(rawYear || "0"), 10) || 0;

  return {
    id: currentRow?.id || "",
    mid: Number(values.mid || baseFilm?.id || 0),
    name: String(baseFilm?.name || "").trim(),
    cName: String(baseFilm?.cName || "").trim(),
    year: parsedYear,
    poster: isCustom && customPic ? customPic : livePic,
    picture: isCustom && customPic ? customPic : livePic,
    pictureSlide: finalSlide,
    customPicture: isCustom ? customPic : "",
    sort: currentRow?.sort ?? 0,
    remark: selectedFilm?.remarks || (!isReplacingFilm ? (currentRow?.remark || "") : ""),
    isCustomPic: isCustom,
    source: currentRow?.source || "manual",
    isPinned: false,
    score: scrapedData?.score ?? (currentRow?.score || selectedFilm?.score || 0),
    blurb: scrapedData?.blurb ?? (currentRow?.blurb || (selectedFilm as any)?.blurb || ""),
    tmdbId: scrapedData?.tmdbId ?? (currentRow?.tmdbId || (selectedFilm as any)?.tmdbId || 0),
  };
}

export function computeTmdbPrefillUpdates(
  data: any,
  fields: string[] | undefined,
  currentValues: BannerFormValues,
  selectedFilm: FilmOption | null,
): Partial<BannerFormValues> {
  const shouldFill = (name: string, ...aliases: string[]) => {
    if (!fields || fields.length === 0) return true;
    return [name, ...aliases].some((key) => fields.includes(key));
  };

  const updates: Partial<BannerFormValues> = {};
  const fillPoster = shouldFill("poster", "picture") && Boolean(data.picture);
  const fillSlide = shouldFill("backdrop", "pictureSlide") && Boolean(data.pictureSlide);

  if (fillPoster || fillSlide) {
    updates.isCustomPic = true;
    if (fillPoster) {
      updates.picture = data.picture;
    } else {
      updates.picture = currentValues.picture || selectedFilm?.picture || "";
    }
    if (fillSlide) {
      updates.pictureSlide = data.pictureSlide;
    } else {
      updates.pictureSlide = currentValues.pictureSlide || selectedFilm?.pictureSlide || "";
    }
  }

  return updates;
}

export async function fetchFilmOption(mid: number): Promise<FilmOption | null> {
  try {
    const resp = await ApiGet("/films/play-info", { id: mid, stored: 1 });
    const d = resp?.data?.detail;
    if (resp?.code === 0 && d) {
      const desc = d.descriptor || {};
      const cName = d.cName || desc.cName || "";
      const year = d.year || (desc.year ? String(desc.year) : "") || "";
      const area = d.area || desc.area || "";
      const remarks = d.remarks || desc.remarks || "";
      const director = d.director || desc.director || "";
      const actor = d.actor || desc.actor || "";
      const rawScore = d.score ?? desc.dbScore;
      const score = Number(rawScore) || 0;

      return {
        id: d.id || mid,
        name: d.name || "",
        cName,
        year,
        remarks,
        picture: d.picture || "",
        pictureSlide: d.pictureSlide || "",
        area,
        director,
        actor,
        label: d.name || "",
        value: d.id || mid,
        isScraped: Boolean(d.isScraped),
        score,
      };
    }
  } catch {}
  return null;
}

export interface BannerPreviewParams {
  currentRow: BannerRecord | null;
  selectedFilm: FilmOption | null;
  isReplacingFilm: boolean;
  watchedPicture?: string;
  watchedPictureSlide?: string;
  watchedIsCustomPic?: boolean;
}

export interface BannerPreviewState {
  currentMid?: number;
  previewName: string;
  previewCategory: string;
  previewYear: string | number;
  previewRemark: string;
  previewArea: string;
  previewDirector: string;
  previewActor: string;
  isScraped: boolean;
  liveFilmPic: string;
  liveFilmSlide: string;
  previewPicture: string;
  previewSlide: string;
}

export function computeBannerPreview({
  currentRow,
  selectedFilm,
  isReplacingFilm,
  watchedPicture,
  watchedPictureSlide,
  watchedIsCustomPic,
}: BannerPreviewParams): BannerPreviewState {
  const activeFilm = isReplacingFilm ? selectedFilm : (selectedFilm || currentRow);
  const currentMid = selectedFilm?.id || (!isReplacingFilm ? currentRow?.mid : undefined);
  const previewName = activeFilm?.name || "未选择影片";
  const previewCategory = activeFilm?.cName || "未分类";
  const previewYear = activeFilm?.year || "未知年份";
  const previewRemark = (isReplacingFilm ? selectedFilm?.remarks : (selectedFilm?.remarks || currentRow?.remark)) || "暂无";
  const previewArea = (isReplacingFilm ? selectedFilm?.area : (selectedFilm?.area || currentRow?.area)) || "未知地区";
  const previewDirector = (isReplacingFilm ? selectedFilm?.director : (selectedFilm?.director || currentRow?.director)) || "暂无";
  const previewActor = (isReplacingFilm ? selectedFilm?.actor : (selectedFilm?.actor || currentRow?.actor)) || "暂无";
  const isScraped = Boolean(isReplacingFilm ? selectedFilm?.isScraped : (selectedFilm?.isScraped ?? currentRow?.isScraped));

  const filmPic = (selectedFilm?.picture || "").trim();
  const filmSlideRaw = (selectedFilm?.pictureSlide || "").trim();
  const liveFilmPic = filmPic;
  const liveFilmSlide = filmSlideRaw && filmSlideRaw !== filmPic ? filmSlideRaw : "";

  const previewPicture = !watchedIsCustomPic
    ? (liveFilmPic || (!isReplacingFilm ? resolvePreviewPicture(currentRow) : ""))
    : (watchedPicture || liveFilmPic);

  const previewSlide = !watchedIsCustomPic
    ? liveFilmSlide
    : (watchedPictureSlide?.trim() || "");

  return {
    currentMid,
    previewName,
    previewCategory,
    previewYear,
    previewRemark,
    previewArea,
    previewDirector,
    previewActor,
    isScraped,
    liveFilmPic,
    liveFilmSlide,
    previewPicture,
    previewSlide,
  };
}
