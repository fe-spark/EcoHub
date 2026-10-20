import assert from "node:assert/strict";
import { test } from "node:test";
import { applyRecordRetry, applyRecordSnapshot } from "./record-retry-state";
import { FAILURE_RECORD_STATUS, type FailRecord } from "./types";

function row(id: number, status = FAILURE_RECORD_STATUS.pending): FailRecord {
  return {
    ID: id,
    originName: "站",
    originId: "s",
    pageNumber: 1,
    hour: 0,
    cause: "old",
    status,
    retryCount: 1,
    CreatedAt: "",
    UpdatedAt: "",
  };
}

test("入队后只把当前记录标成重试中", () => {
  const view = applyRecordRetry([row(1), row(2)], new Set(), {
    id: 1,
    retrying: true,
    removed: false,
    hasResult: false,
    status: 0,
    retryCount: 0,
    cause: "",
  }, -1);
  assert.deepEqual([...view.retrying], [1]);
  assert.equal(view.records[0].cause, "old");
  assert.equal(view.refetch, false);
});

test("失败结果写回本行并解除重试中", () => {
  const view = applyRecordRetry([row(1), row(2)], new Set([1]), {
    id: 1,
    retrying: false,
    removed: false,
    hasResult: true,
    status: FAILURE_RECORD_STATUS.failed,
    retryCount: 3,
    cause: "recover_fetch: down",
  }, -1);
  assert.equal(view.retrying.size, 0);
  assert.equal(view.records.length, 2);
  assert.equal(view.records[0].status, FAILURE_RECORD_STATUS.failed);
  assert.equal(view.records[0].retryCount, 3);
  assert.equal(view.records[0].cause, "recover_fetch: down");
  assert.equal(view.records[1].cause, "old");
  assert.equal(view.refetch, false);
});

test("成功删除当前行并要求补页", () => {
  const view = applyRecordRetry([row(1), row(2)], new Set([1]), {
    id: 1,
    retrying: false,
    removed: true,
    hasResult: true,
    status: FAILURE_RECORD_STATUS.success,
    retryCount: 1,
    cause: "",
  }, -1);
  assert.deepEqual(view.records.map((item) => item.ID), [2]);
  assert.equal(view.refetch, true);
});

test("结果与当前状态筛选不一致时移出并补页", () => {
  const view = applyRecordRetry([row(1)], new Set([1]), {
    id: 1,
    retrying: false,
    removed: false,
    hasResult: true,
    status: FAILURE_RECORD_STATUS.failed,
    retryCount: 3,
    cause: "x",
  }, FAILURE_RECORD_STATUS.pending);
  assert.equal(view.records.length, 0);
  assert.equal(view.refetch, true);
});

test("未真正执行时只取消重试中", () => {
  const view = applyRecordRetry([row(1)], new Set([1]), {
    id: 1,
    retrying: false,
    removed: false,
    hasResult: false,
    status: 0,
    retryCount: 0,
    cause: "",
  }, -1);
  assert.equal(view.retrying.size, 0);
  assert.equal(view.records[0].status, FAILURE_RECORD_STATUS.pending);
  assert.equal(view.records[0].cause, "old");
  assert.equal(view.refetch, false);
});

test("快照只保留当前页仍在队列中的 id", () => {
  const next = applyRecordSnapshot([1, 3], [1, 2, 9]);
  assert.deepEqual([...next], [1]);
});
