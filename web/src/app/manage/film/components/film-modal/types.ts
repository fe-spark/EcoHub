export interface FilmModalProps {
  open: boolean;
  filmId?: number | null;
  defaultSourceId?: string;
  defaultCid?: number;
  onClose: () => void;
  onSuccess: () => void;
}
