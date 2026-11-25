import assert from "node:assert/strict";
import { test } from "node:test";
import {
  collectErrorReason,
  formatBatchSourceStat,
  formatCollectFilms,
  formatSourceWindow,
  getHourLabel,
  parseCollectError,
} from "./record-format";
import { resolveBatchStatus } from "./types";

test("批次状态直接遵循账单状态并兼容历史 done", () => {
  assert.equal(resolveBatchStatus({ status: "done" }), "success");
  assert.equal(resolveBatchStatus({ status: "partial" }), "partial");
  assert.equal(resolveBatchStatus({ status: "partial_stopped" }), "partial_stopped");
  assert.equal(resolveBatchStatus({ status: "interrupted" }), "interrupted");
});

test("站点窗口和批次不同才单独显示", () => {
  assert.equal(formatSourceWindow(48, 24), "48 小时");
  assert.equal(formatSourceWindow(24, 24), "");
  assert.equal(formatSourceWindow(-1, 24), "全量");
});

test("单片更新不显示多余跨度文案", () => {
  assert.equal(getHourLabel(0, { trigger: "single_update" }), "");
  assert.equal(getHourLabel(24, { trigger: "manual" }), "采集今日");
  assert.equal(getHourLabel(-1, { trigger: "cron" }), "全量采集");
});

test("无产出统一写无更新，运行中写更新中", () => {
  assert.equal(formatCollectFilms(0, { status: "success" }), "无更新");
  assert.equal(formatCollectFilms(0, { status: "failed" }), "无更新");
  assert.equal(formatCollectFilms(0, { status: "stopped" }), "无更新");
  assert.equal(formatCollectFilms(0, { status: "running" }), "更新中");
  assert.equal(formatCollectFilms(0, { status: "interrupted" }), "未结算");
  assert.equal(formatCollectFilms(2, { status: "success" }), "+2 部");
});

test("没有明细或状态单一时不重复输出状态文案", () => {
  const noDetail = formatBatchSourceStat({ status: "interrupted", sourceCount: 2 }, []);
  assert.equal(noDetail.primary, "共 2 站");
  assert.equal(noDetail.secondary, "");
  assert.equal(noDetail.tone, "warn");

  const allDone = formatBatchSourceStat(
    { status: "success", sourceCount: 3 },
    [{ status: "done" }, { status: "success" }, { status: "done" }],
  );
  assert.equal(allDone.primary, "共 3 站");
  assert.equal(allDone.secondary, "");
  assert.equal(allDone.tone, "ok");
});

test("站点结果混合时输出拆分统计", () => {
  const mixed = formatBatchSourceStat(
    { status: "interrupted", sourceCount: 2 },
    [{ status: "interrupted" }, { status: "done" }],
  );
  assert.equal(mixed.primary, "共 2 站");
  assert.equal(mixed.secondary, "1 成功 · 1 异常中断");
  assert.equal(mixed.tone, "warn");

  const details = [
    ...Array.from({ length: 11 }, () => ({ status: "done" })),
    { status: "stopped" },
  ];
  const stoppedBatch = formatBatchSourceStat({ status: "partial_stopped", sourceCount: 12 }, details);
  assert.equal(stoppedBatch.primary, "共 12 站");
  assert.equal(stoppedBatch.secondary, "11 成功 · 1 已停止");
  assert.equal(stoppedBatch.tone, "warn");
});

test("异常解析剥离前缀与重复的 url= 后缀并匹配摘要标签", () => {
  const eofErr =
    '站点 樱花 共 3 页失败。抓取失败: Get "https://a.test/api?pg=2": EOF, url=https://a.test/api?pg=2';
  assert.equal(collectErrorReason(eofErr), 'Get "https://a.test/api?pg=2": EOF');
  assert.deepEqual(parseCollectError(eofErr), {
    summary: "连接中断",
    detail: "EOF",
    method: "GET",
    url: "https://a.test/api?pg=2",
  });

  const legacyTruncated =
    '共 11 页失败（第 1, 2... 页）Get "https://b.test/api": context deadline exceeded, url=https://b.test/api...';
  assert.deepEqual(parseCollectError(legacyTruncated), {
    summary: "请求超时",
    detail: "context deadline exceeded",
    method: "GET",
    url: "https://b.test/api",
  });
});
