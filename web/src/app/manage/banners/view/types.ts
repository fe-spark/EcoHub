export type BannerRecord = {
  id: string;
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
  hits?: number;
  blurb?: string;
  actor?: string;
  director?: string;
  isScraped?: boolean;
  tmdbId?: number;
  source?: string;
  isPinned?: boolean;
};

export interface BannerSnapshot {
  formValues: BannerFormValues;
  selectedFilm: FilmOption | null;
}

export const MAX_BANNER_COUNT = 12;

export type BannerFormValues = {
  mid?: number;
  name: string;
  cName: string;
  year?: number;
  picture?: string;
  pictureSlide?: string;
  customPicture?: string;
  sort?: number;
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
