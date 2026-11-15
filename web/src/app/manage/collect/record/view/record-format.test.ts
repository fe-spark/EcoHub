import assert from "node:assert/strict";
import { test } from "node:test";
import {
  formatBatchSourceStat,
  formatCollectFilms,
  formatSourceWindow,
} from "./record-format";

test("站点窗口和批次不同才单独显示", () => {
  assert.equal(formatSourceWindow(48, 24), "48 小时");
  assert.equal(formatSourceWindow(24, 24), "");
  assert.equal(formatSourceWindow(-1, 24), "全量");
});

test("无产出统一写无更新，运行中写更新中", () => {
  assert.equal(formatCollectFilms(0, { status: "success" }), "无更新");
  assert.equal(formatCollectFilms(0, { status: "failed" }), "无更新");
  assert.equal(formatCollectFilms(0, { status: "stopped" }), "无更新");
  assert.equal(formatCollectFilms(0, { status: "running" }), "更新中");
  assert.equal(formatCollectFilms(0, { status: "interrupted" }), "未结算");
  assert.equal(formatCollectFilms(2, { status: "success" }), "+2 部");
});

test("没有明细时只显示站数和批次状态", () => {
  const stat = formatBatchSourceStat(
    { status: "interrupted", sourceCount: 2, successSources: 1 },
    [],
  );
  assert.equal(stat.primary, "2 站");
  assert.equal(stat.secondary, "异常中断");
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
  assert.equal(stat.secondary, "1 成功 · 1 异常中断");
  assert.equal(stat.tone, "warn");
});

test("进行中的站按采集中心状态分开写", () => {
  const stat = formatBatchSourceStat(
    { status: "running", sourceCount: 2 },
    [{ status: "running" }, { status: "sealing" }],
  );
  assert.equal(stat.primary, "2 站");
  assert.equal(stat.secondary, "1 采集中 · 1 待检索");
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

