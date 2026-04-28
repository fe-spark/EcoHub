import type { DataNode } from "antd/es/tree";

export interface FilmClassNode {
  id: number;
  pid: number;
  name: string;
  alias?: string;
  show: boolean;
  sort?: number;
  children?: FilmClassNode[];
}

export interface MappingRuleRecord {
  id: number;
  group: string;
  raw: string;
  target: string;
  matchType: string;
  remarks: string;
}

export interface PagingState {
  current: number;
  pageSize: number;
  total: number;
}

export interface ConflictCheckResult {
  id: number;
  group: string;
  raw: string;
  target: string;
  matchType: string;
}

export interface RuleFormValues {
  group: string;
  raw: string;
  target: string;
  matchType: "exact" | "regex";
  remarks?: string;
}

export interface ClassTreeDataNode extends DataNode {
  key: string;
  title: string;
  rawNode: FilmClassNode;
  children?: ClassTreeDataNode[];
}

export interface TreeDropNode extends ClassTreeDataNode {
  pos: string;
}

export const ROOT_GROUP = "CategoryRoot";
export const SUB_GROUP = "CategorySub";
export const CATEGORY_GROUPS = [ROOT_GROUP, SUB_GROUP];

export const regexPreviewSamples = [
  "电视剧",
  "连续剧",
  "国产剧",
  "日本剧",
  "日剧",
  "国漫",
  "国产动漫",
  "日韩综艺",
  "体育赛事",
  "篮球",
];

const groupLabelMap: Record<string, string> = {
  [ROOT_GROUP]: "一级分类规则",
  [SUB_GROUP]: "二级分类规则",
};

export function resolveGroupLabel(group: string) {
  return groupLabelMap[group] || group;
}

export function normalizeRuleRecord(record: Record<string, unknown>): MappingRuleRecord {
  return {
    id: Number(record.id ?? record.ID ?? 0),
    group: String(record.group ?? record.Group ?? ""),
    raw: String(record.raw ?? record.Raw ?? ""),
    target: String(record.target ?? record.Target ?? ""),
    matchType: String(record.matchType ?? record.MatchType ?? "exact"),
    remarks: String(record.remarks ?? record.Remarks ?? ""),
  };
}

export function normalizeTree(nodes: FilmClassNode[], parentId = 0): FilmClassNode[] {
  return nodes.map((node, index) => ({
    ...node,
    pid: parentId,
    sort: index + 1,
    children: normalizeTree(node.children || [], node.id),
  }));
}

export function cloneTree(nodes: FilmClassNode[]): FilmClassNode[] {
  return nodes.map((node) => ({
    ...node,
    children: cloneTree(node.children || []),
  }));
}

export function flattenTree(nodes: FilmClassNode[]): FilmClassNode[] {
  return nodes.flatMap((node) => [node, ...flattenTree(node.children || [])]);
}

export function collectStats(nodes: FilmClassNode[]) {
  const flat = flattenTree(nodes);
  return {
    total: flat.length,
    roots: nodes.length,
    children: flat.filter((node) => node.pid > 0).length,
    hidden: flat.filter((node) => !node.show).length,
  };
}

export function serializeTree(nodes: FilmClassNode[]) {
  return nodes.map((node) => ({
    id: node.id,
    name: node.name,
    show: node.show,
    children: serializeTree(node.children || []),
  }));
}

export function findTreeNode(nodes: FilmClassNode[], id: number): FilmClassNode | null {
  for (const node of nodes) {
    if (node.id === id) {
      return node;
    }
    const child = findTreeNode(node.children || [], id);
    if (child) {
      return child;
    }
  }
  return null;
}

export function updateTreeNodeShow(nodes: FilmClassNode[], id: number, show: boolean): FilmClassNode[] {
  return nodes.map((node) => {
    if (node.id === id) {
      return {
        ...node,
        show,
        children: updateTreeNodeShow(node.children || [], id, show),
      };
    }
    return {
      ...node,
      children: updateTreeNodeShow(node.children || [], id, show),
    };
  });
}

export function removeTreeNode(nodes: FilmClassNode[], id: number): FilmClassNode[] {
  return nodes
    .filter((node) => node.id !== id)
    .map((node) => ({
      ...node,
      children: removeTreeNode(node.children || [], id),
    }));
}

export function buildNodeKey(id: number) {
  return `node-${id}`;
}

export function parseRuleList(resp: Record<string, any>, current: number, pageSize: number) {
  const data = resp?.data || {};
  const list = Array.isArray(data.list)
    ? data.list
    : Array.isArray(data.records)
      ? data.records
      : Array.isArray(data.items)
        ? data.items
        : [];
  return {
    rules: list.map((item: Record<string, unknown>) => normalizeRuleRecord(item)),
    paging: {
      current: Number(data.current ?? data.page ?? current),
      pageSize: Number(data.pageSize ?? data.size ?? pageSize),
      total: Number(data.total ?? list.length),
    } satisfies PagingState,
  };
}

function reorderList<T>(items: T[], fromIndex: number, toIndex: number) {
  const next = items.slice();
  const [moved] = next.splice(fromIndex, 1);
  next.splice(toIndex, 0, moved);
  return next;
}

export function resolveDropOffset(dropPosition: number, nodePos: string) {
  const currentIndex = Number(nodePos.split("-").pop() || 0);
  return dropPosition - currentIndex;
}

export function moveNodeWithinList<T extends { id: number }>(items: T[], dragId: number, dropId: number, placeAfter: boolean) {
  const fromIndex = items.findIndex((item) => item.id === dragId);
  const targetIndex = items.findIndex((item) => item.id === dropId);
  if (fromIndex < 0 || targetIndex < 0) {
    return null;
  }

  let nextIndex = targetIndex + (placeAfter ? 1 : 0);
  if (fromIndex < nextIndex) {
    nextIndex -= 1;
  }
  if (fromIndex === nextIndex) {
    return null;
  }

  return reorderList(items, fromIndex, nextIndex);
}

export function buildTreeData(nodes: FilmClassNode[]): ClassTreeDataNode[] {
  return nodes.map((node) => ({
    key: buildNodeKey(node.id),
    title: node.name,
    rawNode: node,
    children: buildTreeData(node.children || []),
  }));
}
