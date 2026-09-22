export interface ProxyConfigValues {
  enabled: boolean;
  proxyUrl: string;
  scope: "all" | "custom";
  sourceIds: string[];
}

export interface CollectSourceOption {
  id: string;
  name: string;
  grade?: number;
  state?: boolean;
}
