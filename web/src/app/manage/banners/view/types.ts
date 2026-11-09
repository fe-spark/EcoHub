export type BannerImageMode = "follow" | "custom";

/** 图片、简介、评分、年份：覆盖值或片库值。 */
export type BannerValues = {
  poster: string;
  backdrop: string;
  blurb: string;
  score: number;
  year: number;
};

export type BannerRecord = {
  id: string;
  sourceId?: string;
  imageMode?: BannerImageMode;
  overrides?: BannerValues;
  library?: BannerValues;
  mid: number;
  name: string;
  cName: string;
  year?: number;
  remark?: string;
  poster: string;
  picture: string;
  pictureSlide?: string;
  customPicture?: string;
  sort?: number;
  isCustomPic?: boolean;
  area?: string;
  score?: number;
  blurb?: string;
  actor?: string;
  director?: string;
  isScraped?: boolean;
  tmdbId?: number;
  mediaType?: string;
  /** 影片已不在片库，或在该站已没有播放线路：首页不展示，后台灰显，可删除、参与排序。 */
  unavailable?: boolean;
  pendingScrape?: boolean;
  scrapeState?: "pending" | "scraped" | "miss" | "none";
};

/** 写接口的字段；没给的字段不改，空值清除覆盖。 */
export type BannerFields = {
  mid?: number;
  imageMode?: BannerImageMode;
  poster?: string;
  backdrop?: string;
  blurb?: string;
  score?: number;
  year?: number;
  tmdbId?: number;
};

export type BannerListView = {
  sourceId: string;
  revision: string;
  items: BannerRecord[];
};

export type BannerSkipped = {
  categoryId: number;
  reason: "not_site_category" | "not_enough";
  requested: number;
  got: number;
};

export type BannerGenerateResult = {
  revision: string;
  items: BannerRecord[];
  skipped: BannerSkipped[];
};

export interface BannerSnapshot {
  formValues: BannerFormValues;
  selectedFilm: FilmOption | null;
}

export type BannerFormValues = {
  mid?: number;
  picture?: string;
  pictureSlide?: string;
  customPicture?: string;
  isCustomPic?: boolean;
};

export type FilmOption = {
  id: number;
  name?: string;
  cName?: string;
  year?: string | number;
  remarks?: string;
  picture?: string;
  pictureSlide?: string;
  area?: string;
  director?: string;
  actor?: string;
  label: string;
  value: number;
  isScraped?: boolean;
  score?: number;
  hits?: number;
};

export type EditorMode = "create" | "edit";
export type UploadFieldName = "picture" | "pictureSlide";

export function resolveEditablePicture(record?: Partial<BannerRecord> | null): string {
  if (!record) {
    return "";
  }

  return record.customPicture || (record.isCustomPic ? record.picture : "") || "";
}

export function resolvePreviewPicture(
  record?: BannerRecord | FilmOption | null,
): string {
  if (!record) {
    return "";
  }

  const primaryPicture = record.picture || "";
  if (primaryPicture) {
    return primaryPicture;
  }

  if ("poster" in record && record.poster) {
    return record.poster;
  }

  return "";
}
