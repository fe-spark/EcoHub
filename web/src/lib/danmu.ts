/**
 * 弹幕对接模块（dandanplay 风格接口，兼容 logvar/danmu-api 等自建服务）
 *
 * 接口约定：
 *   GET {base}/search/episodes?anime=<标题>
 *        → { animes: [{ animeTitle, episodes: [{ episodeId, episodeTitle }] }] }
 *   GET {base}/comment/{episodeId}?withRelated=true&chConvert=1
 *        → { comments: [{ p: "时间,模式,颜色,[来源]", m: "文本" }] }
 *
 * 配置来源：后台「系统设置 → 弹幕配置」写入的 localStorage（全局生效、无需后端）。
 *   - enabled：弹幕总开关
 *   - apiUrl ：弹幕服务地址。留空 = 走同源代理 /danmu-api/*（由 Next 服务端转发到弹幕服务），
 *              此时内网/外网/HTTPS 场景都能用且无跨域问题；
 *              填完整地址（如 http://192.168.1.100:9321）= 浏览器直连该服务。
 */

/** 同源代理路径前缀（对应 next.config 中的 rewrites 规则） */
export const API_PATH = "/danmu-api";

const REQUEST_TIMEOUT = 12000;
/** 单集弹幕上限，防止极端大文件拖垮渲染 */
const MAX_COMMENTS = 30000;

const LS_KEY_ENABLED = "ecohub_danmu_enabled";
const LS_KEY_API_URL = "ecohub_danmu_api_url";

/** 弹幕配置（后台可改） */
export interface DanmuConfig {
  /** 总开关，默认开启 */
  enabled: boolean;
  /** 弹幕服务地址；留空 = 走同源代理 */
  apiUrl: string;
}

const DEFAULT_CONFIG: DanmuConfig = { enabled: true, apiUrl: "" };

function normalizeUrl(url: string): string {
  return (url || "").trim().replace(/\/+$/, "");
}

/** 读取弹幕配置（同步，localStorage 足够快） */
export function getDanmuConfig(): DanmuConfig {
  if (typeof window === "undefined") return { ...DEFAULT_CONFIG };
  try {
    const rawEnabled = localStorage.getItem(LS_KEY_ENABLED);
    return {
      // 未写入过（null）视为默认开启；写入过则按写入值
      enabled: rawEnabled === null ? DEFAULT_CONFIG.enabled : rawEnabled === "1",
      apiUrl: normalizeUrl(localStorage.getItem(LS_KEY_API_URL) || ""),
    };
  } catch {
    return { ...DEFAULT_CONFIG };
  }
}

/** 写入弹幕配置 */
export function setDanmuConfig(cfg: DanmuConfig): void {
  if (typeof window === "undefined") return;
  try {
    localStorage.setItem(LS_KEY_ENABLED, cfg.enabled ? "1" : "0");
    const url = normalizeUrl(cfg.apiUrl);
    if (url) localStorage.setItem(LS_KEY_API_URL, url);
    else localStorage.removeItem(LS_KEY_API_URL);
  } catch {
    /* ignore */
  }
}

/**
 * 同步读取已缓存的配置（供渲染/回调期同步判断，未加载时返回 localStorage 值）。
 * 与 getDanmuConfig 等价，命名保留以兼容调用方语义。
 */
export function getDanmuConfigSync(): DanmuConfig {
  return getDanmuConfig();
}

/** 拼出最终的请求前缀：配置了地址则直连，否则走同源代理 */
export function resolveDanmuBase(cfg?: DanmuConfig): string {
  const c = cfg || getDanmuConfig();
  return c.apiUrl ? `${c.apiUrl}${API_PATH}` : API_PATH;
}

async function getJson<T>(url: string, external = false): Promise<T> {
  const ctrl = new AbortController();
  const timer = setTimeout(() => ctrl.abort(), REQUEST_TIMEOUT);
  try {
    // 直连第三方弹幕服务时不需要携带本站 cookie
    const resp = await fetch(url, {
      signal: ctrl.signal,
      credentials: external ? "omit" : "same-origin",
    });
    if (!resp.ok) throw new Error(`HTTP ${resp.status}`);
    return (await resp.json()) as T;
  } finally {
    clearTimeout(timer);
  }
}

interface DanEpisode {
  episodeId: number;
  episodeTitle: string;
}

interface DanAnime {
  animeId: number;
  animeTitle: string;
  episodes: DanEpisode[];
}

interface DanComment {
  cid: number;
  /** "0.00,1,16777215,[qq]" → 时间(秒),dandanplay模式,颜色(十进制),来源 */
  p?: string;
  m: string;
}

/** artplayer-plugin-danmuku 弹幕项 */
export interface DanmakuItem {
  text: string;
  time: number;
  /** 0=滚动 1=顶部 2=底部 */
  mode: 0 | 1 | 2;
  color: string;
  border?: boolean;
}

/**
 * 从剧集标题解析集号（1 基）。
 * 例："【qq】 庆余年第二季_01" → 1；"第12话" → 12；"HD1080P第05集" → 5。
 * 排除与 P/K 等清晰度后缀相连的数字（1080P / 4K）。
 */
function parseEpisodeNumber(raw: string): number | null {
  if (!raw) return null;
  const matches = [...raw.matchAll(/(?<!\d)(\d{1,4})(?!\d)/g)];
  let result: number | null = null;
  for (const m of matches) {
    const num = Number.parseInt(m[1], 10);
    if (!Number.isFinite(num) || num <= 0 || num > 2000) continue;
    // 排除 1080P/720P/4K/2160P 等清晰度数字
    const after = raw[m.index! + m[1].length] || "";
    const before = raw[(m.index! || 1) - 1] || "";
    if (/[pPkK]/.test(after) || /[pPkK]/.test(before)) continue;
    result = num;
  }
  return result;
}

/** 在搜索结果中定位目标集的 episodeId */
function resolveEpisodeId(animes: DanAnime[], episodeIndex: number): number | null {
  if (!animes.length) return null;
  const target = episodeIndex + 1; // 1 基集号

  // ① 各候选剧集标题里精确匹配集号
  for (const anime of animes) {
    for (const ep of anime.episodes || []) {
      if (parseEpisodeNumber(ep.episodeTitle || "") === target) return ep.episodeId;
    }
  }
  // ② 兜底：第一个条目按序取（电影/单集场景 episodeIndex=0）
  const first = animes[0]?.episodes || [];
  if (first.length) {
    return first[Math.min(episodeIndex, first.length - 1)]?.episodeId ?? null;
  }
  return null;
}

/** dandanplay 颜色(十进制) → '#rrggbb' */
function toHexColor(dec: number): string {
  return `#${(dec || 16777215).toString(16).padStart(6, "0")}`;
}

/** dandanplay 模式 → artplayer 模式：1滚动→0，5顶部→1，4底部→2，其余按滚动 */
function toArtMode(d: number): 0 | 1 | 2 {
  if (d === 5) return 1;
  if (d === 4) return 2;
  return 0;
}

/**
 * 按片名与集索引（0 基）拉取并转换弹幕列表。
 * 任何一步失败都会抛错，由调用方决定提示方式。
 * 后台关闭弹幕时返回空数组（调用方据此跳过加载）。
 */
export async function loadDanmakuFor(title: string, episodeIndex: number): Promise<DanmakuItem[]> {
  const cfg = getDanmuConfig();
  if (!cfg.enabled) return [];
  if (!title) throw new Error("empty title");

  const base = resolveDanmuBase(cfg);
  const external = Boolean(cfg.apiUrl);

  const searchUrl = `${base}/search/episodes?anime=${encodeURIComponent(title)}`;
  const searchResp = await getJson<{ success?: boolean; animes?: DanAnime[] }>(searchUrl, external);
  const animes = searchResp?.animes || [];
  const episodeId = resolveEpisodeId(animes, episodeIndex);
  if (!episodeId) throw new Error("no matched episode");

  const commentUrl = `${base}/comment/${episodeId}?withRelated=true&chConvert=1`;
  const commentResp = await getJson<{ comments?: DanComment[] }>(commentUrl, external);
  const comments = commentResp?.comments || [];

  const items: DanmakuItem[] = [];
  for (const c of comments) {
    if (!c?.m) continue;
    const parts = (c.p || "").split(",");
    const time = Number.parseFloat(parts[0]) || 0;
    const mode = toArtMode(Number.parseInt(parts[1], 10) || 1);
    const color = toHexColor(Number.parseInt(parts[2], 10));
    items.push({ text: c.m, time, mode, color });
    if (items.length >= MAX_COMMENTS) break;
  }
  items.sort((a, b) => a.time - b.time);
  return items;
}

/**
 * 后台「连通性测试」用：按给定配置探一次弹幕服务。
 * 返回匹配到的集数与弹幕条数，便于判断地址是否可用。
 */
export async function probeDanmuService(
  cfg: DanmuConfig,
  sampleTitle = "庆余年",
): Promise<{ episodeId: number; count: number }> {
  const base = resolveDanmuBase(cfg);
  const external = Boolean(cfg.apiUrl);
  const searchResp = await getJson<{ animes?: DanAnime[] }>(
    `${base}/search/episodes?anime=${encodeURIComponent(sampleTitle)}`,
    external,
  );
  const animes = searchResp?.animes || [];
  const episodeId = resolveEpisodeId(animes, 0);
  if (!episodeId) throw new Error("未匹配到剧集");
  const commentResp = await getJson<{ comments?: DanComment[] }>(
    `${base}/comment/${episodeId}?withRelated=true&chConvert=1`,
    external,
  );
  return { episodeId, count: (commentResp?.comments || []).length };
}