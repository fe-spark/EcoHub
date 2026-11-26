export interface ChartOption {
  key: string;
  title: string;
}

export interface RecommendCategoryOption {
  id: number;
  name: string;
  filmCount?: number;
}

export interface RecommendAdminItem {
  itemId: string;
  groupId: string;
  sourceId: string;
  mid: number;
  sort: number;
  doubanScore?: string;
  name: string;
  picture: string;
  year: string;
  cName: string;
  area: string;
  remarks: string;
  score?: number;
  unavailable?: boolean;
  categoryHidden?: boolean;
}

export interface RecommendAdminGroup {
  id: string;
  sourceId: string;
  title: string;
  sort: number;
  enabled: boolean;
  autoSync: boolean;
  syncChartKey: string;
  categoryId: number;
  categoryName?: string;
  categoryHidden?: boolean;
  syncLimit: number;
  lastSyncedAt?: string;
  items: RecommendAdminItem[];
}

export interface RecommendSourceOption {
  id: string;
  name: string;
  state?: boolean;
}
