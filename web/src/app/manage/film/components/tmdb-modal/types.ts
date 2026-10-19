export interface TmdbCandidate {
  id: number;
  mediaType: "movie" | "tv";
  title: string;
  originalTitle: string;
  releaseDate: string;
  year: string;
  poster: string;
  backdrop: string;
  voteAverage: number;
  overview: string;
}

export interface TmdbModalProps {
  open: boolean;
  mid?: number;
  initialName?: string;
  initialYear?: string | number;
  onClose: () => void;
  onSuccess?: () => void;
  onPrefill?: (data: any, fields?: string[]) => void;
}

export const FIELD_OPTIONS = [
  { label: "海报", value: "poster" },
  { label: "背景图", value: "backdrop" },
  { label: "简介", value: "overview" },
  { label: "原名", value: "subTitle" },
  { label: "主演", value: "actor" },
  { label: "导演", value: "director" },
  { label: "年份", value: "year" },
  { label: "评分", value: "score" },
  { label: "标签", value: "tag" },
];
