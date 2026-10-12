export interface ProxyModulesValues {
  spider: boolean;
  tmdb: boolean;
  notify: boolean;
  upgrade: boolean;
}

export interface ProxyConfigValues {
  enabled: boolean;
  proxyUrl: string;
  modules: ProxyModulesValues;
}
