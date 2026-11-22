import { ApiGet, ApiPost } from "@/lib/client-api";
import {
  normalizeRawSourceCategories,
  normalizeRuleRecord,
  normalizeTree,
  parseRuleList,
  type FilmClassNode,
  type MappingRuleRecord,
  type RawSourceCategory,
} from "./types";

export async function getFilmClassTree(sourceId = "") {
  const resp = await ApiGet("/manage/film/class/tree", {
    scoped: 1,
    sourceId,
  });
  const sources = Array.isArray(resp.data?.sources) ? resp.data.sources : [];
  return {
    resp,
    tree: normalizeTree((resp.data?.children || []) as FilmClassNode[]),
    sources: sources as { id: string; name: string; state?: boolean }[],
    defaultSourceId: String(resp.data?.defaultSourceId || ""),
    currentSourceId: String(resp.data?.currentSourceId || sourceId || ""),
    primarySourceId: String(
      resp.data?.primarySourceId ??
        resp.data?.currentSourceId ??
        resp.data?.defaultSourceId ??
        sourceId ??
        "",
    ),
  };
}

export async function saveFilmClassTree(children: FilmClassNode[], sourceId: string) {
  return ApiPost("/manage/film/class/tree", { children, sourceId });
}

export async function getCategoryRules(page: number, pageSize: number, keyword: string, group: string, sourceId: string) {
  const resp = await ApiGet("/manage/mapping/rule/list", {
    current: page,
    pageSize,
    group,
    keyword: keyword.trim(),
    sourceId,
  });
  return {
    resp,
    parsed: parseRuleList(resp, page, pageSize),
  };
}

export interface CategoryRuleContextData {
  sourceName: string;
  rawCategories: RawSourceCategory[];
  rules: MappingRuleRecord[];
}

export async function getCategoryRuleContext(sourceId: string): Promise<{
  resp: Awaited<ReturnType<typeof ApiGet>>;
  context: CategoryRuleContextData;
}> {
  const resp = await ApiGet("/manage/mapping/rule/context", { sourceId });
  const data = (resp.data || {}) as Record<string, unknown>;
  const rulesList = Array.isArray(data.rules) ? data.rules : [];
  return {
    resp,
    context: {
      sourceName: String(data.sourceName ?? ""),
      rawCategories: normalizeRawSourceCategories(data.rawCategories),
      rules: rulesList.map((item) => normalizeRuleRecord(item as Record<string, unknown>)),
    },
  };
}

export async function saveCategoryRule(payload: {
  id?: number;
  sourceId: string;
  group: string;
  raw: string;
  target: string;
  matchType: string;
  remarks: string;
}) {
  return ApiPost("/manage/mapping/rule/save", payload);
}

export async function deleteCategoryRule(id: number) {
  return ApiPost("/manage/mapping/rule/delete", { ids: [id] });
}
