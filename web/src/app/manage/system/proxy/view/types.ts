export interface ProxyModulesValues {
  spider: boolean;
  tmdb: boolean;
  notify: boolean;
  upgrade: boolean;
  danmaku: boolean;
}

export interface ProxyConfigValues {
  enabled: boolean;
  proxyUrl: string;
  modules: ProxyModulesValues;
}
