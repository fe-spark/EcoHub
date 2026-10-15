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
    // 严禁拿竖版封面/海报填充横屏大图。若等于封面图，直接视为无横图
    const validSlide = (rawSlide && rawSlide !== rawPic && rawSlide !== rawPoster) ? rawSlide : "";
    return {
      mid: currentRow.mid,
      name: currentRow.name,
      cName: currentRow.cName,
      year: currentRow.year,
      isCustomPic: isCustom,
      picture: isCustom ? resolveEditablePicture(currentRow) : "",
      pictureSlide: isCustom ? validSlide : "",
      sort: currentRow.sort ?? 0,
    };
  }
  return { isCustomPic: false, sort: 0 };
}

interface BuildPayloadParams {
  values: BannerFormValues;
  currentRow: BannerRecord | null;
  selectedFilm: FilmOption | null;
  isReplacingFilm: boolean;
}

export function buildBannerPayload({
  values,
  currentRow,
  selectedFilm,
  isReplacingFilm,
}: BuildPayloadParams): BannerRecord {
  const isCustom = Boolean(values.isCustomPic);
  const customPic = isCustom ? (values.picture || "").trim() : "";
  const customSlide = isCustom ? (values.pictureSlide || "").trim() : "";
  const baseFilm = isReplacingFilm ? selectedFilm : (selectedFilm || currentRow);
  const livePic = baseFilm?.picture || "";
  const rawLiveSlide = baseFilm?.pictureSlide?.trim() || "";
  // 严格隔离：横屏大图必须是真实横图，绝不可回退为竖屏海报 livePic
  const liveSlide = (rawLiveSlide && rawLiveSlide !== livePic) ? rawLiveSlide : "";
  const finalSlide = isCustom ? customSlide : liveSlide;

  return {
    id: currentRow?.id || "",
    mid: Number(values.mid || baseFilm?.id || 0),
    name: String(values.name || baseFilm?.name || "").trim(),
    cName: String(values.cName || baseFilm?.cName || "").trim(),
    year: values.year,
    poster: isCustom && customPic ? customPic : livePic,
    picture: isCustom && customPic ? customPic : livePic,
    pictureSlide: finalSlide,
    customPicture: isCustom ? customPic : "",
    sort: values.sort ?? currentRow?.sort ?? 0,
    remark: selectedFilm?.remarks || (!isReplacingFilm ? (currentRow?.remark || "") : ""),
    isCustomPic: isCustom,
    source: currentRow?.source || "manual",
    isPinned: currentRow?.isPinned ?? false,
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
  if (shouldFill("name") && data.name) {
    updates.name = data.name;
  }
  if (shouldFill("subTitle", "cName") && (data.subTitle || data.cName)) {
    updates.cName = data.subTitle || data.cName;
  }
  if (shouldFill("year", "releaseDate") && (data.year || data.releaseDate)) {
    const parsedYear = parseInt(String(data.year || data.releaseDate || "0"), 10);
    if (parsedYear > 0) {
      updates.year = parsedYear;
    }
  }

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
    const resp = await ApiGet("/films/play-info", { id: mid });
    const d = resp?.data?.detail;
    if (resp?.code === 0 && d) {
      return {
        id: d.id || mid,
        name: d.name || "",
        cName: d.cName || "",
        year: d.year || "",
        remarks: d.remarks || "",
        picture: d.picture || "",
        pictureSlide: d.pictureSlide || "",
        area: d.area,
        director: d.director,
        actor: d.actor,
        label: d.name || "",
        value: d.id || mid,
        isScraped: d.isScraped,
      };
    }
  } catch {}
  return null;
}

export function mergeTmdbFilmOption(
  prev: FilmOption | null,
  data: any,
  updates: Partial<BannerFormValues>,
  currentMid?: number,
): FilmOption {
  const base: FilmOption = prev || {
    id: currentMid || 0,
    name: data.name || "",
    label: data.name || "",
    value: currentMid || 0,
  };
  return {
    ...base,
    name: updates.name || base.name,
    cName: updates.cName || base.cName,
    year: updates.year || base.year,
    director: data.director || base.director,
    actor: data.actor || base.actor,
  };
}

export interface BannerPreviewParams {
  currentRow: BannerRecord | null;
  selectedFilm: FilmOption | null;
  isReplacingFilm: boolean;
  watchedName?: string;
  watchedCName?: string;
  watchedYear?: number;
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
  watchedName,
  watchedCName,
  watchedYear,
  watchedPicture,
  watchedPictureSlide,
  watchedIsCustomPic,
}: BannerPreviewParams): BannerPreviewState {
  const activeFilm = isReplacingFilm ? selectedFilm : (selectedFilm || currentRow);
  const currentMid = selectedFilm?.id || (!isReplacingFilm ? currentRow?.mid : undefined);
  const previewName = watchedName || activeFilm?.name || "未选择影片";
  const previewCategory = watchedCName || activeFilm?.cName || "未分类";
  const previewYear = watchedYear || activeFilm?.year || "未知年份";
  const previewRemark = (isReplacingFilm ? selectedFilm?.remarks : (selectedFilm?.remarks || currentRow?.remark)) || "暂无";
  const previewArea = (isReplacingFilm ? selectedFilm?.area : (selectedFilm?.area || currentRow?.area)) || "未知地区";
  const previewDirector = (isReplacingFilm ? selectedFilm?.director : (selectedFilm?.director || currentRow?.director)) || "暂无";
  const previewActor = (isReplacingFilm ? selectedFilm?.actor : (selectedFilm?.actor || currentRow?.actor)) || "暂无";
  const isScraped = Boolean(isReplacingFilm ? selectedFilm?.isScraped : (selectedFilm?.isScraped ?? currentRow?.isScraped));

  // 片库原图。轮播记录上的 picture / pictureSlide 可能已是刮削或自定义结果，不能回填到绑定影片。
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
