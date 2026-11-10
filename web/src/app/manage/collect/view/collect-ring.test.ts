import assert from "node:assert/strict";
import { test } from "node:test";
import { resolveRingView } from "./collect-ring-view";
import type { CollectProgress } from "./types";

function progress(partial: Partial<CollectProgress>): CollectProgress {
  return {
    id: "a",
    name: "a",
    total: 10,
    current: 10,
    success: 10,
    failed: 0,
    status: "done",
    ...partial,
  };
}

test("待检索是纯色空环，中间没有数字", () => {
  const view = resolveRingView(progress({ status: "sealing" }));
  assert.deepEqual(view, {
    percent: 0,
    tone: "processing",
    solid: true,
    center: "empty",
    tooltip: "待检索",
  });
});

test("完成时环和勾用同一个成功色", () => {
  const view = resolveRingView(progress({ status: "done" }));
  assert.equal(view?.tone, "success");
  assert.equal(view?.center, "success");
});

test("采集中有失败页时环用错误色，完成后的部分失败仍用警告色", () => {
  const running = resolveRingView(
    progress({ status: "running", current: 4, success: 3, failed: 1 }),
  );
  assert.equal(running?.tone, "error");
  assert.equal(running?.center, "percent");
  const stopping = resolveRingView(
    progress({ status: "stopping", current: 4, success: 3, failed: 1 }),
  );
  assert.equal(stopping?.tone, "error");
  const done = resolveRingView(progress({ status: "done", success: 8, failed: 2 }));
  assert.equal(done?.tone, "warning");
  assert.equal(done?.center, "success");
});

test("全部失败时环和叉用同一个错误色", () => {
  const view = resolveRingView(
    progress({ status: "failed", success: 0, failed: 10, current: 10 }),
  );
  assert.equal(view?.tone, "error");
  assert.equal(view?.center, "fail");
});

test("没有进度时不渲染环", () => {
  assert.equal(resolveRingView(null), null);
});

test("失败页排好序并压成区间，单独放进 failedPages，提示主行只留总数", () => {
  const view = resolveRingView(
    progress({ status: "running", total: 5000, current: 120, success: 114, failed: 6, failedPages: [4765, 4762, 4764, 4763, 9, 5000] }),
  );
  assert.equal(view?.tooltip.includes("4762"), false);
  assert.match(view?.tooltip ?? "", /失败 6 页$/);
  assert.deepEqual(view?.failedPages, { text: "9、4762-4765、5000", count: 6 });
});

test("没有记录失败页时不带 failedPages", () => {
  const view = resolveRingView(progress({ status: "done", success: 8, failed: 2 }));
  assert.equal(view?.failedPages, undefined);
});
