import { ApiGet, ApiPost } from "@/lib/client-api";
import {
  BannerRecord,
  BannerFields,
  BannerFormValues,
  FilmOption,
  EditorMode,
  resolvePreviewPicture,
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
    const isCustom = currentRow.imageMode === "custom";
    return {
      mid: currentRow.mid,
      isCustomPic: isCustom,
      picture: isCustom ? currentRow.overrides?.poster || "" : "",
      pictureSlide: isCustom ? currentRow.overrides?.backdrop || "" : "",
    };
  }
  return { isCustomPic: false };
}

export type BannerScrapedData = {
  score?: number;
  blurb?: string;
  tmdbId?: number;
};

interface BuildFieldsParams {
  values: BannerFormValues;
  currentRow: BannerRecord | null;
  scrapedData: BannerScrapedData;
}

/**
 * 编辑抽屉提交的字段。新增给全量；编辑只给改过的字段。
 * scrapedData 只放 TMDB 填入的值；恢复片库默认时清空为 {}，编辑时据此清除覆盖。
 */
export function buildBannerFields({ values, currentRow, scrapedData }: BuildFieldsParams): BannerFields {
  const isCustom = Boolean(values.isCustomPic);
  const poster = isCustom ? (values.picture || "").trim() : "";
  const backdrop = isCustom ? (values.pictureSlide || "").trim() : "";
  const target: Required<Omit<BannerFields, "year">> = {
    mid: Number(values.mid || 0),
    imageMode: isCustom ? "custom" : "follow",
    poster,
    backdrop,
    blurb: scrapedData.blurb || "",
    score: scrapedData.score || 0,
    tmdbId: scrapedData.tmdbId || 0,
  };
  if (!currentRow) {
    const fields: BannerFields = { mid: target.mid, imageMode: target.imageMode };
    if (isCustom) {
      fields.poster = poster;
      fields.backdrop = backdrop;
    }
    if (target.blurb) fields.blurb = target.blurb;
    if (target.score) fields.score = target.score;
    if (target.tmdbId) fields.tmdbId = target.tmdbId;
    return fields;
  }

  const prev = currentRow.overrides;
  const fields: BannerFields = {};
  if (target.mid !== Number(currentRow.mid)) {
    // 换片后服务端会清掉 TMDB 和文字覆盖，这里按新片重新给。
    fields.mid = target.mid;
    fields.imageMode = target.imageMode;
    if (isCustom) {
      fields.poster = poster;
      fields.backdrop = backdrop;
    }
    if (target.blurb) fields.blurb = target.blurb;
    if (target.score) fields.score = target.score;
    if (target.tmdbId) fields.tmdbId = target.tmdbId;
    return fields;
  }
  if (target.imageMode !== (currentRow.imageMode || "follow")) fields.imageMode = target.imageMode;
  if (isCustom && poster !== (prev?.poster || "")) fields.poster = poster;
  if (isCustom && backdrop !== (prev?.backdrop || "")) fields.backdrop = backdrop;
  if (target.blurb !== (prev?.blurb || "")) fields.blurb = target.blurb;
  if (target.score !== (prev?.score || 0)) fields.score = target.score;
  if (target.tmdbId !== (currentRow.tmdbId || 0)) fields.tmdbId = target.tmdbId;
  return fields;
}

/** 排片预览的一行转成发布字段：只给和片库不同的值。 */
export function deckItemFields(item: BannerRecord): BannerFields {
  const lib = item.library;
  const custom = item.imageMode === "custom" || Boolean(item.isCustomPic);
  const fields: BannerFields = { mid: Number(item.mid), imageMode: custom ? "custom" : "follow" };
  if (custom) {
    fields.poster = (item.customPicture || item.picture || item.poster || "").trim();
    const slide = (item.pictureSlide || "").trim();
    fields.backdrop = slide && slide !== fields.poster ? slide : "";
  }
  if (item.blurb && item.blurb !== (lib?.blurb || "")) fields.blurb = item.blurb;
  if (item.score && item.score !== (lib?.score || 0)) fields.score = item.score;
  if (item.year && item.year !== (lib?.year || 0)) fields.year = item.year;
  if (item.tmdbId) fields.tmdbId = item.tmdbId;
  return fields;
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
