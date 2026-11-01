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
  const records = [row(1), row(2)];
  const view = applyRecordRetry(records, new Set(), {
    id: 1,
    retrying: true,
    hasResult: false,
    status: 0,
    retryCount: 0,
    cause: "",
  });
  assert.deepEqual([...view.retrying], [1]);
  assert.equal(view.records, records);
  assert.equal(view.records[0].cause, "old");
});

test("失败结果写回本行并解除重试中", () => {
  const view = applyRecordRetry([row(1), row(2)], new Set([1]), {
    id: 1,
    retrying: false,
    hasResult: true,
    status: FAILURE_RECORD_STATUS.failed,
    retryCount: 3,
    cause: "recover_fetch: down",
  });
  assert.equal(view.retrying.size, 0);
  assert.equal(view.records.length, 2);
  assert.equal(view.records[0].status, FAILURE_RECORD_STATUS.failed);
  assert.equal(view.records[0].retryCount, 3);
  assert.equal(view.records[0].cause, "recover_fetch: down");
  assert.equal(view.records[1].cause, "old");
});

test("成功结果留在原位并改成重试成功", () => {
  const view = applyRecordRetry([row(1), row(2)], new Set([1]), {
    id: 1,
    retrying: false,
    hasResult: true,
    status: FAILURE_RECORD_STATUS.success,
    retryCount: 1,
    cause: "",
  });
  assert.deepEqual(view.records.map((item) => item.ID), [1, 2]);
  assert.equal(view.records[0].status, FAILURE_RECORD_STATUS.success);
  assert.equal(view.records[0].cause, "");
  assert.equal(view.retrying.size, 0);
});

test("未真正执行时只取消重试中", () => {
  const view = applyRecordRetry([row(1)], new Set([1]), {
    id: 1,
    retrying: false,
    hasResult: false,
    status: 0,
    retryCount: 0,
    cause: "",
  });
  assert.equal(view.retrying.size, 0);
  assert.equal(view.records[0].status, FAILURE_RECORD_STATUS.pending);
  assert.equal(view.records[0].cause, "old");
});

test("快照只保留当前页仍在队列中的 id", () => {
  const next = applyRecordSnapshot([1, 3], [1, 2, 9]);
  assert.deepEqual([...next], [1]);
});
