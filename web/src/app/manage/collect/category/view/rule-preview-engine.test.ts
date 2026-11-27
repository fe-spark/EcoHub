import assert from "node:assert/strict";
import { test } from "node:test";
import { buildFinalCategoryPreview, findLocalRuleConflicts } from "./rule-preview-engine";
import { ROOT_GROUP, SUB_GROUP, parseRuleList, type MappingRuleRecord, type RawSourceCategory } from "./types";

const sampleRaw: RawSourceCategory[] = [
  { id: 1, parentId: 0, name: "福利视频", sort: 2, show: true },
  { id: 2, parentId: 0, name: "主播秀", sort: 2, show: true },
  { id: 3, parentId: 0, name: "电影", sort: 2, show: true },
  { id: 10, parentId: 3, name: "动作片", sort: 1, show: true },
];

const savedRules: MappingRuleRecord[] = [
  { id: 1, sourceId: "s1", group: ROOT_GROUP, raw: "福利.*", target: "成人精选", matchType: "regex", remarks: "" },
  { id: 2, sourceId: "s1", group: SUB_GROUP, raw: ".*视频$", target: "精选短片", matchType: "regex", remarks: "" },
];

test("合成一级分类 ID=0 在同 Sort 下排在原生分类之前，且下沉原一级可继续被二级规则合并", () => {
  const preview = buildFinalCategoryPreview({
    rawCategories: sampleRaw,
    savedRules,
    draft: {
      group: ROOT_GROUP,
      matchType: "exact",
      raw: "主播秀",
      target: "成人精选",
    },
  });

  assert.equal(preview.valid, true);
  assert.equal(preview.hitCount, 1);
  // 「成人精选」为合成组(id=0)，同 sort=2 应排在原生「电影」(id=3) 之前
  assert.equal(preview.tree[0]?.name, "成人精选");
  assert.equal(preview.tree[0]?.synthetic, true);
  assert.equal(preview.tree[0]?.id, 0);
  assert.equal(preview.tree[1]?.name, "电影");
  // 「福利视频」下沉后被已保存的二级正则改名为「精选短片」
  assert.deepEqual(
    preview.tree[0]?.children.map((c) => ({ name: c.name, fromRoot: c.fromRoot })),
    [
      { name: "精选短片", fromRoot: true },
      { name: "主播秀", fromRoot: true },
    ],
  );
});

test("草稿只填原始值、尚未填目标值时保留命中高亮并继续显示已保存正则的效果", () => {
  const preview = buildFinalCategoryPreview({
    rawCategories: [{ id: 1, parentId: 0, name: "福利视频", sort: 1, show: true }],
    savedRules: [
      { id: 1, sourceId: "s1", group: ROOT_GROUP, raw: "福利.*", target: "成人精选", matchType: "regex", remarks: "" },
    ],
    draft: {
      group: ROOT_GROUP,
      matchType: "exact",
      raw: "福利视频",
      target: "",
    },
  });

  assert.equal(preview.hitCount, 1);
  assert.equal(preview.tree[0]?.name, "成人精选");
  assert.equal(preview.tree[0]?.hitByDraft, true);
});

test("草稿正则目标为空时停在原名，不再套用后面的已保存正则", () => {
  const preview = buildFinalCategoryPreview({
    rawCategories: [{ id: 1, parentId: 0, name: "电影", sort: 1, show: true }],
    savedRules: [
      { id: 1, sourceId: "s1", group: ROOT_GROUP, raw: "电影", target: "影片", matchType: "regex", remarks: "" },
      { id: 2, sourceId: "s1", group: ROOT_GROUP, raw: "影", target: "影视", matchType: "regex", remarks: "" },
    ],
    draft: {
      editingId: 1,
      group: ROOT_GROUP,
      matchType: "regex",
      raw: "电影",
      target: "",
    },
  });

  assert.equal(preview.tree[0]?.name, "电影");
  assert.equal(preview.tree[0]?.hitByDraft, true);
});

test("已保存的 (?i) 按不区分大小写生效，RE2 不支持的前瞻不会被静默丢掉", () => {
  const preview = buildFinalCategoryPreview({
    rawCategories: [{ id: 1, parentId: 0, name: "Movie", sort: 1, show: true }],
    savedRules: [
      { id: 1, sourceId: "s1", group: ROOT_GROUP, raw: "(?i)movie", target: "电影", matchType: "regex", remarks: "" },
    ],
    draft: { group: ROOT_GROUP, matchType: "exact", raw: "", target: "" },
  });
  assert.equal(preview.valid, true);
  assert.equal(preview.tree[0]?.name, "电影");

  const rejected = buildFinalCategoryPreview({
    rawCategories: [{ id: 1, parentId: 0, name: "Movie", sort: 1, show: true }],
    savedRules: [
      { id: 1, sourceId: "s1", group: ROOT_GROUP, raw: "(?=movie)", target: "电影", matchType: "regex", remarks: "" },
    ],
    draft: { group: ROOT_GROUP, matchType: "exact", raw: "", target: "" },
  });
  assert.equal(rejected.valid, false);
  assert.equal(rejected.error, "正则表达式不合法");
});

test("同名精确规则只认最后一条，空目标才落到正则", () => {
  const lastWins = buildFinalCategoryPreview({
    rawCategories: [{ id: 1, parentId: 0, name: "电影", sort: 1, show: true }],
    savedRules: [
      { id: 1, sourceId: "s1", group: ROOT_GROUP, raw: "电影", target: "旧名", matchType: "exact", remarks: "" },
      { id: 2, sourceId: "s1", group: ROOT_GROUP, raw: "电影", target: "新名", matchType: "exact", remarks: "" },
    ],
    draft: { group: ROOT_GROUP, matchType: "exact", raw: "", target: "" },
  });
  assert.equal(lastWins.tree[0]?.name, "新名");

  const emptyFallsThrough = buildFinalCategoryPreview({
    rawCategories: [{ id: 1, parentId: 0, name: "电影", sort: 1, show: true }],
    savedRules: [
      { id: 1, sourceId: "s1", group: ROOT_GROUP, raw: "电影", target: "旧名", matchType: "exact", remarks: "" },
      { id: 2, sourceId: "s1", group: ROOT_GROUP, raw: "电影", target: "", matchType: "exact", remarks: "" },
      { id: 3, sourceId: "s1", group: ROOT_GROUP, raw: "电影", target: "影片", matchType: "regex", remarks: "" },
    ],
    draft: { group: ROOT_GROUP, matchType: "exact", raw: "", target: "" },
  });
  assert.equal(emptyFallsThrough.tree[0]?.name, "影片");
});

test("findLocalRuleConflicts 同分组+原始值+匹配方式检出冲突并排除正在编辑的规则", () => {
  const conflicts = findLocalRuleConflicts(savedRules, {
    group: ROOT_GROUP,
    raw: "福利.*",
    matchType: "regex",
  });
  assert.equal(conflicts.length, 1);

  const editingSelf = findLocalRuleConflicts(savedRules, {
    editingId: 1,
    group: ROOT_GROUP,
    raw: "福利.*",
    matchType: "regex",
  });
  assert.equal(editingSelf.length, 0);
});

test("parseRuleList 从嵌套 data.paging 正确解析总条数与页码", () => {
  const parsed = parseRuleList(
    {
      data: {
        list: [{ id: 1, sourceId: "s1", group: ROOT_GROUP, raw: "A", target: "B", matchType: "exact", remarks: "" }],
        paging: { current: 2, pageSize: 10, total: 35 },
        totals: { [ROOT_GROUP]: 20, [SUB_GROUP]: 15 },
      },
    },
    1,
    10,
  );
  assert.equal(parsed.paging.current, 2);
  assert.equal(parsed.paging.pageSize, 10);
  assert.equal(parsed.paging.total, 35);
});

test("支持 (?i:...) 内联标志与 syntheticStates 同步隐藏/排序状态", () => {
  const preview = buildFinalCategoryPreview({
    rawCategories: [{ id: 1, parentId: 0, name: "HDMovie", sort: 2, show: true }],
    savedRules: [
      { id: 1, sourceId: "s1", group: ROOT_GROUP, raw: "(?i:movie)", target: "高清专区", matchType: "regex", remarks: "" },
    ],
    draft: { group: ROOT_GROUP, matchType: "exact", raw: "", target: "" },
    syntheticStates: { 高清专区: { show: false, sort: 9 } },
  });
  assert.equal(preview.valid, true);
  assert.equal(preview.tree[0]?.name, "高清专区");
  assert.equal(preview.tree[0]?.show, false);
  assert.equal(preview.tree[0]?.sort, 9);

  // 转义后的 \(\?i:movie\) 应按字面量匹配，不被误当作内联标志组替换
  const escapedPreview = buildFinalCategoryPreview({
    rawCategories: [{ id: 1, parentId: 0, name: "(?i:movie)", sort: 1, show: true }],
    savedRules: [
      { id: 1, sourceId: "s1", group: ROOT_GROUP, raw: "\\(\\?i:movie\\)", target: "字面匹配", matchType: "regex", remarks: "" },
    ],
    draft: { group: ROOT_GROUP, matchType: "exact", raw: "", target: "" },
  });
  assert.equal(escapedPreview.valid, true);
  assert.equal(escapedPreview.tree[0]?.name, "字面匹配");
});
