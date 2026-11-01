export interface MovieUrlInfo {
  episode: string;
  link: string;
}

export interface PlayLine {
  id: string;
  sourceId?: string;
  name: string;
  linkList: MovieUrlInfo[];
}

export interface FilmDrawerProps {
  open: boolean;
  filmId?: number | null;
  defaultSourceId?: string;
  defaultCid?: number;
  onClose: () => void;
  onSuccess: () => void;
}
