import assert from "node:assert/strict";
import { test } from "node:test";
import {
  computeCollectQueueProgress,
  groupCollectQueues,
  isActiveQueueView,
  queueEndNotice,
  sameCollectProgress,
} from "./collect-queue";
import type { FilmSource } from "./types";

function site(id: string, progress?: FilmSource["progress"]): FilmSource {
  return {
    id,
    name: id,
    uri: "",
    state: true,
    sort: 0,
    progress: progress ?? null,
  };
}

function progress(
  id: string,
  queueId: string,
  status: string,
  pages: { total: number; success: number; failed: number },
): FilmSource["progress"] {
  return {
    id,
    name: id,
    current: pages.success + pages.failed,
    total: pages.total,
    success: pages.success,
    failed: pages.failed,
    status,
    queueId,
  };
}

test("groupCollectQueues 按服务端 queueId 分组", () => {
  const groups = groupCollectQueues([
    site("默认", progress("默认", "q-batch-12", "done", { total: 10, success: 10, failed: 0 })),
    site("HD(IK)", progress("HD(IK)", "q-single-retry", "running", { total: 5, success: 1, failed: 0 })),
    site("HD(BF)", progress("HD(BF)", "q-batch-12", "running", { total: 3, success: 0, failed: 0 })),
  ]);
  assert.deepEqual(
    groups.map((g) => ({ queueId: g.queueId, sourceIds: g.sourceIds })),
    [
      { queueId: "q-batch-12", sourceIds: ["默认", "HD(BF)"] },
      { queueId: "q-single-retry", sourceIds: ["HD(IK)"] },
    ],
  );
});

test("没有 queueId 的站点不进采集队列", () => {
  const groups = groupCollectQueues([
    site("a", {
      id: "a",
      name: "HD(SN)",
      total: 10,
      current: 4,
      success: 0,
      failed: 32,
      status: "failed",
    }),
    site("b", progress("b", "q-1", "running", { total: 10, success: 2, failed: 0 })),
  ]);
  assert.deepEqual(
    groups.map((g) => g.sourceIds),
    [["b"]],
  );
});

test("computeCollectQueueProgress 按页数汇总进度", () => {
  const view = computeCollectQueueProgress(
    ["a", "b"],
    [
      site("a", progress("a", "q-1", "running", { total: 100, success: 30, failed: 10 })),
      site("b", progress("b", "q-1", "done", { total: 50, success: 50, failed: 0 })),
    ],
  );
  assert.equal(view?.percent, 60);
  assert.equal(view?.pageDone, 90);
  assert.equal(view?.pageTotal, 150);
  assert.equal(view?.doneCount, 1);
  assert.equal(view?.runningCount, 1);
  assert.equal(view?.failed, 10);
  assert.equal(view?.running, true);
  assert.equal(view?.sealing, false);
});

test("采集完的站点进入待检索，队列保持活跃并复用封口进度", () => {
  const sites = [
    site("a", progress("a", "q-1", "sealing", { total: 10, success: 10, failed: 0 })),
    site("b", progress("b", "q-1", "sealing", { total: 5, success: 5, failed: 0 })),
  ];
  const waiting = computeCollectQueueProgress(["a", "b"], sites, {
    status: "waiting",
    percent: 0,
    current: 0,
    total: 0,
  });
  assert.equal(waiting?.sealing, true);
  assert.equal(waiting?.running, false);
  assert.equal(waiting?.sealingCount, 2);
  assert.equal(waiting?.percent, 0);
  assert.equal(isActiveQueueView(waiting!), true);

  const indexing = computeCollectQueueProgress(["a", "b"], sites, {
    status: "indexing",
    percent: 42,
    current: 8400,
    total: 20000,
  });
  assert.equal(indexing?.sealIndexing, true);
  assert.equal(indexing?.percent, 42);
  assert.equal(indexing?.sealCurrent, 8400);
  assert.equal(indexing?.sealTotal, 20000);
});

test("封口完成收成 done 后队列才算结束", () => {
  const view = computeCollectQueueProgress(
    ["a"],
    [site("a", progress("a", "q-1", "done", { total: 10, success: 10, failed: 0 }))],
  );
  assert.equal(view?.running, false);
  assert.equal(view?.sealing, false);
  assert.equal(view?.percent, 100);
  assert.equal(isActiveQueueView(view!), false);
  assert.deepEqual(queueEndNotice(view!), { level: "success", text: "采集完成" });
});

test("封口完成后进度停在 100%，不回退到抓页进度", () => {
  const view = computeCollectQueueProgress(
    ["a"],
    [
      site("a", {
        ...progress("a", "q-1", "stopped", { total: 10, success: 4, failed: 0 }),
        sealed: true,
      }),
    ],
  );
  assert.equal(view?.sealed, true);
  assert.equal(view?.sealing, false);
  assert.equal(view?.percent, 100);
  assert.equal(isActiveQueueView(view!), false);
});

test("个别站终止时整队仍按完成收尾", () => {
  const view = computeCollectQueueProgress(
    ["a", "b"],
    [
      site("a", progress("a", "q-1", "done", { total: 10, success: 10, failed: 0 })),
      site("b", progress("b", "q-1", "stopped", { total: 10, success: 3, failed: 0 })),
    ],
  );
  assert.equal(view?.stoppedCount, 1);
  assert.equal(view?.doneCount, 1);
  assert.deepEqual(queueEndNotice(view!), { level: "success", text: "采集完成" });
});

test("停止和失败分别给出终止与异常提示", () => {
  const stopped = computeCollectQueueProgress(
    ["a"],
    [site("a", progress("a", "q-1", "stopped", { total: 10, success: 4, failed: 0 }))],
  );
  assert.deepEqual(queueEndNotice(stopped!), { level: "info", text: "采集已停止" });

  const failed = computeCollectQueueProgress(
    ["a"],
    [site("a", progress("a", "q-1", "failed", { total: 10, success: 0, failed: 10 }))],
  );
  assert.deepEqual(queueEndNotice(failed!), { level: "error", text: "采集失败" });
});

test("sameCollectProgress 只比较字段，忽略对象身份", () => {
  const a = progress("a", "q-1", "running", { total: 10, success: 2, failed: 0 });
  const b = progress("a", "q-1", "running", { total: 10, success: 2, failed: 0 });
  assert.equal(sameCollectProgress(a, b), true);
  assert.equal(sameCollectProgress(a, null), false);
  assert.equal(sameCollectProgress(null, null), true);
  assert.equal(sameCollectProgress(a, { ...b!, status: "sealing" }), false);
  assert.equal(
    sameCollectProgress({ ...a, failedPages: [3, 7] }, { ...b, failedPages: [3, 7] }),
    true,
  );
  assert.equal(
    sameCollectProgress({ ...a, failedPages: [3, 7] }, { ...b, failedPages: [3, 8] }),
    false,
  );
});
