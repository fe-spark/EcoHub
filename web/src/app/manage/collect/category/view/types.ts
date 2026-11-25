export interface FilmClassNode {
  id: number;
  pid: number;
  name: string;
  alias?: string;
  show: boolean;
  sort?: number;
  filmCount?: number;
  children?: FilmClassNode[];
  memberIds?: number[];
}

export interface MappingRuleRecord {
  id: number;
  sourceId: string;
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

export interface RuleFormValues {
  group: string;
  raw: string;
  target: string;
  matchType: "exact" | "regex";
  remarks?: string;
}

export interface RawSourceCategory {
  id: number;
  parentId: number;
  name: string;
  sort: number;
  show: boolean;
}

export interface PreviewCategoryChild {
  key: string;
  id: number;
  name: string;
  rawNames: string[];
  fromRoot: boolean;
  show: boolean;
  sort: number;
  affected: boolean;
  hitByDraft: boolean;
}

export interface PreviewCategoryRoot {
  key: string;
  id: number;
  name: string;
  rawNames: string[];
  synthetic: boolean;
  show: boolean;
  sort: number;
  affected: boolean;
  hitByDraft: boolean;
  children: PreviewCategoryChild[];
}

export interface RuleFullPreview {
  hasDraftRaw: boolean;
  hasDraftTarget: boolean;
  draftTarget: string;
  valid: boolean;
  error: string;
  hitCount: number;
  rootCount: number;
  subCount: number;
  tree: PreviewCategoryRoot[];
}

export const ALL_CATEGORY_GROUP = "CategoryAll";
export const ROOT_GROUP = "CategoryRoot";
export const SUB_GROUP = "CategorySub";
export const CATEGORY_GROUPS = [ROOT_GROUP, SUB_GROUP];
export const CATEGORY_FILTER_GROUPS = [ALL_CATEGORY_GROUP, ROOT_GROUP, SUB_GROUP];

export const emptyRuleFullPreview: RuleFullPreview = {
  hasDraftRaw: false,
  hasDraftTarget: false,
  draftTarget: "",
  valid: true,
  error: "",
  hitCount: 0,
  rootCount: 0,
  subCount: 0,
  tree: [],
};

const groupLabelMap: Record<string, string> = {
  [ALL_CATEGORY_GROUP]: "全部分组",
  [ROOT_GROUP]: "一级分类规则",
  [SUB_GROUP]: "二级分类规则",
};

export function resolveGroupLabel(group: string) {
  return groupLabelMap[group] || group;
}

export function normalizeRuleRecord(record: Record<string, unknown>): MappingRuleRecord {
  return {
    id: Number(record.id ?? record.ID ?? 0),
    sourceId: String(record.sourceId ?? record.SourceId ?? ""),
    group: String(record.group ?? record.Group ?? ""),
    raw: String(record.raw ?? record.Raw ?? ""),
    target: String(record.target ?? record.Target ?? ""),
    matchType: String(record.matchType ?? record.MatchType ?? "exact"),
    remarks: String(record.remarks ?? record.Remarks ?? ""),
  };
}

export function normalizeRawSourceCategories(list: unknown): RawSourceCategory[] {
  if (!Array.isArray(list)) {
    return [];
  }
  return list.map((item) => {
    const row = (item || {}) as Record<string, unknown>;
    return {
      id: Number(row.id ?? row.ID ?? 0),
      parentId: Number(row.parentId ?? row.ParentID ?? 0),
      name: String(row.name ?? row.Name ?? ""),
      sort: Number(row.sort ?? row.Sort ?? 0),
      show: row.show !== false,
    };
  });
}

export function normalizeTree(nodes: FilmClassNode[], parentId = 0): FilmClassNode[] {
  return nodes.map((node, index) => {
    const children = normalizeTree(node.children || [], node.id);
    const { children: _children, ...nodeInfo } = node;
    return children.length > 0
      ? { ...nodeInfo, pid: parentId, sort: index + 1, children }
      : { ...nodeInfo, pid: parentId, sort: index + 1 };
  });
}

export function cloneTree(nodes: FilmClassNode[]): FilmClassNode[] {
  return nodes.map((node) => {
    const children = cloneTree(node.children || []);
    const { children: _children, ...nodeInfo } = node;
    return children.length > 0 ? { ...nodeInfo, children } : nodeInfo;
  });
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
    memberIds: node.memberIds || [],
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

export function updateTreeNodeVisibility(nodes: FilmClassNode[], id: number, show: boolean): FilmClassNode[] {
  return nodes.map((node) => {
    const children = updateTreeNodeVisibility(node.children || [], id, show);
    const { children: _children, ...nodeInfo } = node;
    const next = children.length > 0 ? { ...nodeInfo, children } : nodeInfo;
    return node.id === id ? { ...next, show } : next;
  });
}

function reorderList<T>(items: T[], fromIndex: number, toIndex: number) {
  const next = items.slice();
  const [moved] = next.splice(fromIndex, 1);
  next.splice(toIndex, 0, moved);
  return next;
}

function moveWithinList<T extends { id: number }>(items: T[], dragId: number, dropId: number) {
  const fromIndex = items.findIndex((item) => item.id === dragId);
  const targetIndex = items.findIndex((item) => item.id === dropId);
  if (fromIndex < 0 || targetIndex < 0 || fromIndex === targetIndex) {
    return null;
  }
  return reorderList(items, fromIndex, targetIndex);
}

export function moveCategoryWithinSameParent(nodes: FilmClassNode[], dragId: number, dropId: number) {
  const movedRoots = moveWithinList(nodes, dragId, dropId);
  if (movedRoots) {
    return normalizeTree(movedRoots);
  }

  let changed = false;
  const next = nodes.map((node) => {
    const children = node.children || [];
    const movedChildren = moveWithinList(children, dragId, dropId);
    if (movedChildren) {
      changed = true;
      return { ...node, children: movedChildren };
    }
    return node;
  });

  return changed ? normalizeTree(next) : nodes;
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
  const rawPaging = (data.paging || {}) as Record<string, unknown>;
  const totalsRaw = data.totals as Record<string, unknown> | undefined;
  const totals = totalsRaw
    ? {
        [ROOT_GROUP]: Number(totalsRaw[ROOT_GROUP] ?? 0),
        [SUB_GROUP]: Number(totalsRaw[SUB_GROUP] ?? 0),
      }
    : undefined;
  return {
    rules: list.map((item: Record<string, unknown>) => normalizeRuleRecord(item)),
    totals,
    paging: {
      current: Number(rawPaging.current ?? data.current ?? data.page ?? current),
      pageSize: Number(rawPaging.pageSize ?? data.pageSize ?? data.size ?? pageSize),
      total: Number(rawPaging.total ?? data.total ?? list.length),
    } satisfies PagingState,
  };
}
