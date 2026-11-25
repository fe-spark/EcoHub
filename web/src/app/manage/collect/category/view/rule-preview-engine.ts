import {
  ROOT_GROUP,
  SUB_GROUP,
  type MappingRuleRecord,
  type PreviewCategoryChild,
  type PreviewCategoryRoot,
  type RawSourceCategory,
  type RuleFullPreview,
} from "./types";

export interface DraftRuleInput {
  editingId?: number;
  group: string;
  matchType: string;
  raw: string;
  target: string;
}

interface CompiledRule {
  isDraft: boolean;
  raw: string;
  target: string;
  matchType: "exact" | "regex";
  pattern?: RegExp;
}

interface RuleApplyResult {
  name: string;
  hitByDraft: boolean;
}

interface WorkingRow {
  id: number;
  parentId: number;
  name: string;
  rawName: string;
  sort: number;
  show: boolean;
  fromRoot: boolean;
  hitByDraft: boolean;
}

// shortcut: 支持行首 (?ims) 及非转义的 (?ims:...) 分组标志；复杂局部大小写切换若与 RE2 差异仍大时改回服务端预览。
function compilePreviewRegex(raw: string): RegExp {
  let source = raw;
  let flags = "";
  const lead = source.match(/^\(\?([ims]+)\)/);
  if (lead) {
    flags = lead[1];
    source = source.slice(lead[0].length);
  }

  let out = "";
  let escaped = false;
  for (let i = 0; i < source.length; i += 1) {
    const ch = source[i];
    if (escaped) {
      if (ch >= "1" && ch <= "9") {
        throw new Error("re2");
      }
      out += ch;
      escaped = false;
      continue;
    }
    if (ch === "\\") {
      out += ch;
      escaped = true;
      continue;
    }
    if (source.startsWith("(?", i)) {
      const next = source[i + 2];
      if (next === "=" || next === "!" || next === "<") {
        throw new Error("re2");
      }
      const inlineMatch = source.slice(i).match(/^\(\?([ims]+):/);
      if (inlineMatch) {
        for (const f of inlineMatch[1]) {
          if (!flags.includes(f)) {
            flags += f;
          }
        }
        out += "(?:";
        i += inlineMatch[0].length - 1;
        continue;
      }
    }
    out += ch;
  }
  return new RegExp(out, flags);
}

function compileRuleGroup(rules: MappingRuleRecord[], draft: DraftRuleInput, targetGroup: string): {
  exact: CompiledRule[];
  regex: CompiledRule[];
  regexError: string;
} {
  const exact: CompiledRule[] = [];
  const regex: CompiledRule[] = [];
  let regexError = "";

  const draftRaw = draft.raw.trim();
  const draftRecord: (MappingRuleRecord & { isDraft?: boolean }) | null =
    draft.group === targetGroup && draftRaw
      ? {
          id: draft.editingId ?? Number.MAX_SAFE_INTEGER,
          sourceId: "",
          group: targetGroup,
          raw: draftRaw,
          target: draft.target.trim(),
          matchType: draft.matchType === "regex" ? "regex" : "exact",
          remarks: "",
          isDraft: true,
        }
      : null;

  const merged: Array<MappingRuleRecord & { isDraft?: boolean }> = [];
  let insertedDraft = false;
  for (const item of rules) {
    if (item.id === draft.editingId) {
      if (draftRecord) {
        merged.push(draftRecord);
        insertedDraft = true;
      }
      continue;
    }
    merged.push({ ...item, isDraft: false });
  }
  if (draftRecord && !insertedDraft) {
    merged.push(draftRecord);
  }

  for (const rule of merged) {
    if (rule.group !== targetGroup) {
      continue;
    }
    const raw = rule.raw.trim();
    if (!raw) {
      continue;
    }
    const target = rule.target.trim();
    if (rule.matchType === "regex") {
      try {
        // 不使用 g 标志，避免 RegExp.test 维护 lastIndex 状态导致隔行误判
        const pattern = compilePreviewRegex(raw);
        regex.push({ isDraft: Boolean(rule.isDraft), raw, target, matchType: "regex", pattern });
      } catch {
        regexError = "正则表达式不合法";
      }
      continue;
    }
    exact.push({ isDraft: Boolean(rule.isDraft), raw, target, matchType: "exact" });
  }

  return { exact, regex, regexError };
}

function applyCompiledRules(
  compiled: { exact: CompiledRule[]; regex: CompiledRule[] },
  sourceName: string,
): RuleApplyResult {
  const name = sourceName.trim();
  if (!name) {
    return { name: "", hitByDraft: false };
  }
  let hitByDraft = false;
  // 精确规则与服务端 map 一致：同名只认最后一条；目标为空才继续走正则。
  let exactHit: CompiledRule | undefined;
  for (const rule of compiled.exact) {
    if (rule.raw === name) {
      exactHit = rule;
    }
  }
  if (exactHit?.isDraft) {
    hitByDraft = true;
  }
  if (exactHit?.target) {
    return { name: exactHit.target, hitByDraft };
  }
  for (const rule of compiled.regex) {
    if (!rule.pattern?.test(name)) {
      continue;
    }
    if (rule.isDraft) {
      hitByDraft = true;
    }
    if (rule.target) {
      return { name: rule.target, hitByDraft };
    }
    // 命中但目标为空：停在原名。草稿也一样，否则后面的规则会把预览改掉、保存却不变。
    return { name, hitByDraft };
  }
  return { name, hitByDraft };
}

function pickRepresentative(members: WorkingRow[]): WorkingRow {
  let rep = members[0];
  for (const row of members) {
    if (row.show !== rep.show) {
      if (row.show) rep = row;
      continue;
    }
    if (row.id < rep.id) {
      rep = row;
    }
  }
  return rep;
}

function uniqueNames(items: WorkingRow[]): string[] {
  const seen = new Set<string>();
  const out: string[] = [];
  for (const item of items) {
    if (item.rawName && !seen.has(item.rawName)) {
      seen.add(item.rawName);
      out.push(item.rawName);
    }
  }
  return out;
}

function sortGroups<T extends { sort: number; id: number }>(list: T[]): T[] {
  return list.slice().sort((a, b) => (a.sort !== b.sort ? a.sort - b.sort : a.id - b.id));
}

function groupChildRows(rows: WorkingRow[]): PreviewCategoryChild[] {
  const order: string[] = [];
  const buckets = new Map<string, WorkingRow[]>();
  for (const row of rows) {
    const key = row.name.trim() || row.rawName;
    if (!buckets.has(key)) {
      order.push(key);
      buckets.set(key, []);
    }
    buckets.get(key)!.push(row);
  }
  const children: PreviewCategoryChild[] = order.map((name) => {
    const members = buckets.get(name)!;
    const rep = pickRepresentative(members);
    const hitByDraft = members.some((m) => m.hitByDraft);
    const rawNames = uniqueNames(members);
    const fromRoot = members.some((m) => m.fromRoot);
    return {
      key: `sub-${rep.id}-${name}`,
      id: rep.id,
      name,
      rawNames,
      fromRoot,
      show: rep.show,
      sort: rep.sort,
      affected: hitByDraft,
      hitByDraft,
    };
  });
  return sortGroups(children);
}

/**
 * 在本地复刻后端 `groupPublicTypes` 分组逻辑，输入原始分类与全量规则，返回最终完整分类树与命中计数。
 */
export function buildFinalCategoryPreview(params: {
  rawCategories: RawSourceCategory[];
  savedRules: MappingRuleRecord[];
  draft: DraftRuleInput;
  syntheticStates?: Record<string, { show: boolean; sort: number }>;
}): RuleFullPreview {
  const { rawCategories, savedRules, draft, syntheticStates } = params;
  const draftRaw = draft.raw.trim();
  const draftTarget = draft.target.trim();
  const rootRules = compileRuleGroup(savedRules, draft, ROOT_GROUP);
  const subRules = compileRuleGroup(savedRules, draft, SUB_GROUP);
  const regexError = rootRules.regexError || subRules.regexError;

  const childrenOf = new Map<number, RawSourceCategory[]>();
  const roots: RawSourceCategory[] = [];
  for (const row of rawCategories) {
    if (row.parentId > 0) {
      if (!childrenOf.has(row.parentId)) {
        childrenOf.set(row.parentId, []);
      }
      childrenOf.get(row.parentId)!.push(row);
    } else {
      roots.push(row);
    }
  }

  interface RootBucket {
    native: WorkingRow[];
    renamed: WorkingRow[];
    all: WorkingRow[];
  }
  const order: string[] = [];
  const buckets = new Map<string, RootBucket>();

  for (const row of roots) {
    const applied = applyCompiledRules(rootRules, row.name);
    const groupName = applied.name || row.name;
    if (!buckets.has(groupName)) {
      order.push(groupName);
      buckets.set(groupName, { native: [], renamed: [], all: [] });
    }
    const bucket = buckets.get(groupName)!;
    const item: WorkingRow = {
      id: row.id,
      parentId: row.parentId,
      name: groupName,
      rawName: row.name,
      sort: row.sort,
      show: row.show,
      fromRoot: false,
      hitByDraft: applied.hitByDraft,
    };
    bucket.all.push(item);
    if (groupName !== row.name) {
      bucket.renamed.push(item);
    } else {
      bucket.native.push(item);
    }
  }

  let hitCount = 0;
  let subCount = 0;
  const rootNodes: PreviewCategoryRoot[] = order.map((name) => {
    const bucket = buckets.get(name)!;
    const synthetic = bucket.native.length === 0;
    const rep = synthetic ? pickRepresentative(bucket.all) : pickRepresentative(bucket.native);
    const rootHit = bucket.all.some((m) => m.hitByDraft);

    const childRows: WorkingRow[] = [];
    for (const renamedRoot of bucket.renamed) {
      const subApplied = applyCompiledRules(subRules, renamedRoot.rawName);
      childRows.push({
        id: renamedRoot.id,
        parentId: renamedRoot.id,
        name: subApplied.name || renamedRoot.rawName,
        rawName: renamedRoot.rawName,
        sort: renamedRoot.sort,
        show: renamedRoot.show,
        fromRoot: true,
        hitByDraft: draft.group === ROOT_GROUP ? renamedRoot.hitByDraft : subApplied.hitByDraft,
      });
    }
    for (const rootItem of bucket.all) {
      for (const child of childrenOf.get(rootItem.id) || []) {
        const subApplied = applyCompiledRules(subRules, child.name);
        childRows.push({
          id: child.id,
          parentId: rootItem.id,
          name: subApplied.name || child.name,
          rawName: child.name,
          sort: child.sort,
          show: child.show,
          fromRoot: false,
          hitByDraft: draft.group === SUB_GROUP ? subApplied.hitByDraft : false,
        });
      }
    }

    const children = groupChildRows(childRows);
    subCount += children.length;
    if (draft.group === ROOT_GROUP) {
      hitCount += bucket.all.filter((m) => m.hitByDraft).length;
    } else if (draft.group === SUB_GROUP) {
      hitCount += childRows.filter((c) => c.hitByDraft).length;
    }

    const rawNames = synthetic ? uniqueNames(bucket.all) : uniqueNames(bucket.native);
    const anyChildHit = children.some((c) => c.hitByDraft);
    const synState = synthetic ? syntheticStates?.[name] : undefined;
    return {
      key: `root-${synthetic ? "syn" : rep.id}-${name}`,
      id: synthetic ? 0 : rep.id,
      name,
      rawNames,
      synthetic,
      show: synthetic ? (synState?.show ?? true) : rep.show,
      sort: synthetic ? (synState?.sort ?? rep.sort) : rep.sort,
      affected: rootHit || anyChildHit,
      hitByDraft: rootHit,
      children,
    };
  });

  const sortedTree = sortGroups(rootNodes);
  return {
    hasDraftRaw: Boolean(draftRaw),
    hasDraftTarget: Boolean(draftTarget),
    draftTarget,
    valid: !regexError,
    error: regexError,
    hitCount,
    rootCount: sortedTree.length,
    subCount,
    tree: sortedTree,
  };
}

/**
 * 本地检测规则冲突（与后端 FindMappingRulesByEffectPoint 口径一致：同分组、同原始值、同匹配方式）。
 */
export function findLocalRuleConflicts(
  savedRules: MappingRuleRecord[],
  draft: { editingId?: number; group: string; raw: string; matchType: string },
): MappingRuleRecord[] {
  const raw = draft.raw.trim();
  const group = draft.group.trim();
  const matchType = draft.matchType === "regex" ? "regex" : "exact";
  if (!raw || !group) {
    return [];
  }
  return savedRules.filter(
    (item) =>
      item.id !== draft.editingId &&
      item.group === group &&
      item.raw.trim() === raw &&
      (item.matchType === "regex" ? "regex" : "exact") === matchType,
  );
}

function escapeRegexLiteral(text: string): string {
  return text.replace(/[.*+?^${}()|[\]\\]/g, "\\$&");
}

/**
 * 点击分类名称快速填入「原始值」；正则模式自动转义并按 `|` 去重拼接。
 */
export function buildNextRawInput(currentRaw: string, matchType: "exact" | "regex", clickedName: string): string {
  const clean = clickedName.trim();
  if (!clean) {
    return currentRaw;
  }
  if (matchType !== "regex") {
    return clean;
  }
  const escaped = escapeRegexLiteral(clean);
  const existing = currentRaw.trim();
  if (!existing) {
    return escaped;
  }
  const parts = existing.split("|").map((p) => p.trim()).filter(Boolean);
  if (parts.includes(escaped) || parts.includes(clean)) {
    return existing;
  }
  return `${existing}|${escaped}`;
}
