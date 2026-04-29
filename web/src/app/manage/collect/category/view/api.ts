import { ApiGet, ApiPost } from "@/lib/client-api";
import { CATEGORY_GROUPS, normalizeRuleRecord, normalizeTree, parseRuleList, type FilmClassNode } from "./types";

export async function getFilmClassTree() {
  const resp = await ApiGet("/manage/film/class/tree");
  return {
    resp,
    tree: normalizeTree((resp.data?.children || []) as FilmClassNode[]),
  };
}

export async function resetFilmClassTree() {
  return ApiPost("/manage/film/class/collect", {});
}

export async function saveFilmClassTree(children: FilmClassNode[]) {
  return ApiPost("/manage/film/class/tree/save", { children });
}

export async function updateFilmClassShow(id: number, show: boolean) {
  return ApiPost("/manage/film/class/update", { id, show });
}

export async function getCategoryRules(page: number, pageSize: number, keyword: string, group: string) {
  const resp = await ApiGet("/manage/mapping/rule/list", {
    current: page,
    pageSize,
    group,
    keyword: keyword.trim(),
  });
  return {
    resp,
    parsed: parseRuleList(resp, page, pageSize),
  };
}

export async function getCategoryRuleTotals() {
  const [rootResp, subResp] = await Promise.all(
    CATEGORY_GROUPS.map((group) =>
      ApiGet("/manage/mapping/rule/list", {
        current: 1,
        pageSize: 1,
        group,
        keyword: "",
      }),
    ),
  );
  return CATEGORY_GROUPS.reduce<Record<string, number>>((totals, group, index) => {
    totals[group] = parseRuleList(index === 0 ? rootResp : subResp, 1, 1).paging.total;
    return totals;
  }, {});
}

export async function checkCategoryRuleConflict(payload: { id?: number; group: string; raw: string; matchType: string }) {
  const resp = await ApiPost("/manage/mapping/rule/check", payload);
  const list = Array.isArray(resp.data?.rules) ? resp.data.rules : Array.isArray(resp.data) ? resp.data : [];
  return {
    resp,
    rules: list.map((item: Record<string, unknown>) => normalizeRuleRecord(item)),
  };
}

export async function saveCategoryRule(payload: {
  id?: number;
  group: string;
  raw: string;
  target: string;
  matchType: string;
  remarks: string;
}) {
  return ApiPost(payload.id ? "/manage/mapping/rule/update" : "/manage/mapping/rule/add", payload);
}

export async function deleteCategoryRule(id: number) {
  return ApiPost("/manage/mapping/rule/del", { id });
}
