import assert from "node:assert/strict";
import { test } from "node:test";
import { decideFilmPageUpdate, isCurrentFilmRequest } from "./film-list-request";

test("过期响应不采用", () => {
  assert.equal(isCurrentFilmRequest(2, 3), false);
  assert.equal(isCurrentFilmRequest(3, 3), true);
});

test("静默刷新不改列表，索引结束再拉一次", () => {
  assert.deepEqual(
    decideFilmPageUpdate({ silent: true, ok: true, indexing: true, wasIndexing: true }),
    {
      applyList: false,
      indexing: true,
      schedulePoll: true,
      refreshList: false,
      clearIndexing: false,
    },
  );
  assert.deepEqual(
    decideFilmPageUpdate({ silent: true, ok: true, indexing: false, wasIndexing: true }),
    {
      applyList: false,
      indexing: false,
      schedulePoll: false,
      refreshList: true,
      clearIndexing: false,
    },
  );
});

test("请求失败关掉构建告警", () => {
  const decision = decideFilmPageUpdate({
    silent: true,
    ok: false,
    indexing: true,
    wasIndexing: true,
  });
  assert.equal(decision.clearIndexing, true);
  assert.equal(decision.applyList, false);
  assert.equal(decision.schedulePoll, false);
});
