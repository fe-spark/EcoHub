import { ApiGet, ApiPost } from "@/lib/client-api";
import {
  CATEGORY_GROUPS,
  emptyRuleMergePreview,
  normalizePreviewHits,
  normalizeRuleRecord,
  normalizeTree,
  parseRuleList,
  type FilmClassNode,
  type RuleMergePreview,
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

export async function getCategoryRuleTotals(sourceId: string) {
  const [rootResp, subResp] = await Promise.all(
    CATEGORY_GROUPS.map((group) =>
      ApiGet("/manage/mapping/rule/list", {
        current: 1,
        pageSize: 1,
        group,
        keyword: "",
        sourceId,
      }),
    ),
  );
  return CATEGORY_GROUPS.reduce<Record<string, number>>((totals, group, index) => {
    totals[group] = parseRuleList(index === 0 ? rootResp : subResp, 1, 1).paging.total;
    return totals;
  }, {});
}

export async function checkCategoryRuleConflict(payload: {
  id?: number;
  sourceId: string;
  group: string;
  raw: string;
  matchType: string;
}) {
  const resp = await ApiPost("/manage/mapping/rule/check", payload);
  const list = Array.isArray(resp.data?.rules) ? resp.data.rules : Array.isArray(resp.data) ? resp.data : [];
  return {
    resp,
    rules: list.map((item: Record<string, unknown>) => normalizeRuleRecord(item)),
  };
}

export async function previewCategoryRule(payload: {
  sourceId: string;
  group: string;
  raw: string;
  matchType: string;
  target: string;
}): Promise<{ resp: Awaited<ReturnType<typeof ApiPost>>; preview: RuleMergePreview }> {
  const resp = await ApiPost("/manage/mapping/rule/preview", payload);
  if (resp.code !== 0) {
    return {
      resp,
      preview: {
        ...emptyRuleMergePreview,
        valid: false,
        error: String(resp.msg || "预览失败"),
      },
    };
  }
  const data = (resp.data || {}) as Record<string, unknown>;
  return {
    resp,
    preview: {
      loading: false,
      valid: true,
      error: "",
      sourceName: String(data.sourceName ?? ""),
      target: String(data.target ?? payload.target),
      hits: normalizePreviewHits(data.hits),
      keep: normalizePreviewHits(data.keep),
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
