import assert from "node:assert/strict";
import { test } from "node:test";
import {
  formatBatchSourceStat,
  formatCollectFilms,
  formatCollectPages,
  formatSourcePages,
  formatSourceWindow,
  splitFailureMarks,
} from "./record-format";

test("停止写明未抓，不把没抓的页显示成失败", () => {
  const pages = formatCollectPages({ status: "stopped", pageSuccess: 3, pageTotal: 10, pageFailed: 0 });
  assert.equal(pages.primary, "已停止");
  assert.equal(pages.secondary, "成功 3 · 未抓 7 · 共 10 页");
  assert.equal(pages.alert, false);
});

test("一页没写出的停止不显示成全部拉完", () => {
  const pages = formatCollectPages({ status: "stopped", pageSuccess: 0, pageTotal: 8, pageFailed: 0 });
  assert.equal(pages.primary, "已停止");
  assert.equal(pages.secondary, "成功 0 · 未抓 8 · 共 8 页");
});

test("真实失败页与未抓差额同时展示", () => {
  const pages = formatCollectPages({ status: "partial", pageSuccess: 4, pageTotal: 10, pageFailed: 2 });
  assert.equal(pages.primary, "2 页失败");
  assert.equal(pages.secondary, "成功 4 · 失败 2 · 未抓 4 · 共 10 页");
  assert.equal(
    formatSourcePages({ status: "failed", pageTotal: 10, successCnt: 4, failedCnt: 2 }),
    "成功 4 · 失败 2 · 未抓 4 · 共 10 页",
  );
});

test("停止前已有失败页时状态仍是已停止", () => {
  const pages = formatCollectPages({ status: "stopped", pageSuccess: 3, pageTotal: 10, pageFailed: 2 });
  assert.equal(pages.primary, "已停止");
  assert.equal(pages.secondary, "成功 3 · 失败 2 · 未抓 5 · 共 10 页");
  assert.equal(pages.alert, true);
});

test("无产出统一写无更新，运行中写更新中", () => {
  assert.equal(formatCollectFilms(0, { status: "success" }), "无更新");
  assert.equal(formatCollectFilms(0, { status: "failed" }), "无更新");
  assert.equal(formatCollectFilms(0, { status: "stopped" }), "无更新");
  assert.equal(formatCollectFilms(0, { status: "running" }), "更新中");
  assert.equal(formatCollectFilms(2, { status: "success" }), "+2 部");
});

test("没有明细时只显示站数和批次状态", () => {
  const stat = formatBatchSourceStat(
    { status: "interrupted", sourceCount: 2, successSources: 1 },
    [],
  );
  assert.equal(stat.primary, "2 站");
  assert.equal(stat.secondary, "中断");
  assert.equal(stat.tone, "warn");
});

test("中断站按中断计数，不写成已停止", () => {
  const stat = formatBatchSourceStat(
    { status: "interrupted", sourceCount: 2, successSources: 0 },
    [
      { status: "interrupted" },
      { status: "done" },
    ],
  );
  assert.equal(stat.primary, "2 站");
  assert.equal(stat.secondary, "1 成功 · 1 中断");
  assert.equal(stat.tone, "warn");
});

test("停止批次的站点列只写站数构成", () => {
  const details = [
    ...Array.from({ length: 11 }, () => ({ status: "done" })),
    { status: "stopped" },
  ];
  const stat = formatBatchSourceStat(
    { status: "stopped", sourceCount: 12, successSources: 11 },
    details,
  );
  assert.equal(stat.primary, "12 站");
  assert.equal(stat.secondary, "11 成功 · 1 已停止");
  assert.equal(stat.tone, "warn");
});

test("站点窗口和批次不同才单独显示", () => {
  assert.equal(formatSourceWindow(48, 24), "48 小时");
  assert.equal(formatSourceWindow(24, 24), "");
  assert.equal(formatSourceWindow(-1, 24), "全量");
});

test("失败页文案可以单独拆出来上色", () => {
  assert.deepEqual(splitFailureMarks("成功 1148 / 共 3380 页，失败 8 页"), [
    { text: "成功 1148 / 共 3380 页，", fail: false },
    { text: "失败 8 页", fail: true },
  ]);
  assert.deepEqual(splitFailureMarks("成功 4 · 失败 2 · 未抓 4 · 共 10 页"), [
    { text: "成功 4 · ", fail: false },
    { text: "失败 2", fail: true },
    { text: " · 未抓 4 · 共 10 页", fail: false },
  ]);
});

test("运行中的个别失败不把成功进度标成整行失败", () => {
  const pages = formatCollectPages({
    status: "running",
    pageSuccess: 17604,
    pageTotal: 65612,
    pageFailed: 8,
  });
  assert.equal(pages.primary, "成功 17604 / 共 65612 页");
  assert.equal(pages.secondary, "拉取中 · 26% · 失败 8");
  assert.equal(pages.alert, false);
});

test("运行中状态明确展示进度而非全部完成", () => {
  const pages = formatCollectPages({ status: "running", pageSuccess: 20620, pageTotal: 26928, pageFailed: 0 });
  assert.equal(pages.primary, "成功 20620 / 共 26928 页");
  assert.equal(pages.secondary, "拉取中 · 76%");
  assert.equal(pages.alert, false);

  const sourcePage = formatSourcePages({ status: "running", pageTotal: 0, successCnt: 0, failedCnt: 0 });
  assert.equal(sourcePage, "探测页码中");
});
