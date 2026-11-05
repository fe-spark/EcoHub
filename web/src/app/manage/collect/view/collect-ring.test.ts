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

test("有失败页时环和中间内容都用警告色", () => {
  const done = resolveRingView(progress({ status: "done", success: 8, failed: 2 }));
  assert.equal(done?.tone, "warning");
  assert.equal(done?.center, "success");
  const running = resolveRingView(
    progress({ status: "running", current: 4, success: 3, failed: 1 }),
  );
  assert.equal(running?.tone, "warning");
  assert.equal(running?.center, "percent");
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
