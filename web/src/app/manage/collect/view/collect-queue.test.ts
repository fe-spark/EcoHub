import assert from "node:assert/strict";
import { test } from "node:test";
import {
  computeCollectQueueProgress,
  finishedQueueViews,
  groupCollectQueues,
  isClientCollectQueueId,
  matchPrevCollectQueueBar,
  nextCollectProgress,
  queueEndNotice,
} from "./collect-queue";
import type { FilmSource } from "./types";

function site(
  id: string,
  progress?: FilmSource["progress"],
): FilmSource {
  return {
    id,
    name: id,
    uri: "",
    state: true,
    sort: 0,
    interval: 0,
    progress: progress ?? null,
  };
}

function starting(id: string, queueId: string): FilmSource["progress"] {
  return {
    id,
    name: id,
    total: 0,
    current: 0,
    success: 0,
    failed: 0,
    status: "starting",
    queueId,
  };
}

test("groupCollectQueues: 单站重采与原批次拆成两条队列", () => {
  const groups = groupCollectQueues([
    site("默认", {
      ...starting("默认", "q-batch-12")!,
      status: "done",
    }),
    site("HD(IK)", starting("HD(IK)", "q-single-retry")),
    site("HD(BF)", {
      ...starting("HD(BF)", "q-batch-12")!,
      status: "running",
    }),
  ]);
  assert.deepEqual(
    groups.map((g) => ({ queueId: g.queueId, sourceIds: g.sourceIds })),
    [
      { queueId: "q-batch-12", sourceIds: ["默认", "HD(BF)"] },
      { queueId: "q-single-retry", sourceIds: ["HD(IK)"] },
    ],
  );
});

test("matchPrevCollectQueueBar: q-local 乐观条接到服务端 queueId", () => {
  const prev = [{ queueId: "q-local-abc", sourceIds: ["默认", "HD(IK)"] }];
  const used = new Set<string>();
  const matched = matchPrevCollectQueueBar(
    { queueId: "q-batch-12", sourceIds: ["默认", "HD(IK)"] },
    prev,
    used,
  );
  assert.equal(matched?.queueId, "q-local-abc");
});

test("matchPrevCollectQueueBar: IK 重采不得并回仍含该站的旧批次条", () => {
  const prev = [
    { queueId: "q-batch-12", sourceIds: ["默认", "HD(IK)", "HD(BF)"] },
  ];
  const used = new Set<string>();
  const matched = matchPrevCollectQueueBar(
    { queueId: "q-single-retry", sourceIds: ["HD(IK)"] },
    prev,
    used,
  );
  assert.equal(matched, undefined);
});

test("matchPrevCollectQueueBar: 同 queueId 仍对齐原条", () => {
  const prev = [{ queueId: "q-batch-12", sourceIds: ["默认", "HD(BF)"] }];
  const used = new Set<string>();
  const matched = matchPrevCollectQueueBar(
    { queueId: "q-batch-12", sourceIds: ["默认", "HD(BF)"] },
    prev,
    used,
  );
  assert.equal(matched?.queueId, "q-batch-12");
});

test("computeCollectQueueProgress 按页数汇总进度", () => {
  const view = computeCollectQueueProgress(
    ["a", "b"],
    [
      site("a", {
        id: "a",
        name: "a",
        total: 100,
        current: 40,
        success: 30,
        failed: 10,
        status: "running",
        queueId: "q-1",
      }),
      site("b", {
        id: "b",
        name: "b",
        total: 50,
        current: 50,
        success: 50,
        failed: 0,
        status: "done",
        queueId: "q-1",
      }),
    ],
  );
  assert.equal(view?.percent, 60);
  assert.equal(view?.pageDone, 90);
  assert.equal(view?.pageTotal, 150);
  assert.equal(view?.doneCount, 1);
  assert.equal(view?.failed, 10);
});

test("队列结束仍留在分组里时也要提示，成功用 success", () => {
  const running = computeCollectQueueProgress(
    ["a"],
    [
      site("a", {
        id: "a",
        name: "a",
        total: 10,
        current: 4,
        success: 4,
        failed: 0,
        status: "running",
        queueId: "q-1",
      }),
    ],
  );
  const done = computeCollectQueueProgress(
    ["a"],
    [
      site("a", {
        id: "a",
        name: "a",
        total: 10,
        current: 10,
        success: 10,
        failed: 0,
        status: "done",
        queueId: "q-1",
      }),
    ],
  );
  const prev = [{ seq: 1, queueId: "q-1", view: running! }];
  const next = [{ seq: 1, queueId: "q-1", view: done! }];
  const ended = finishedQueueViews(prev, next);
  assert.equal(ended.length, 1);
  assert.equal(ended[0]?.running, false);
  assert.deepEqual(queueEndNotice(ended[0]!), { level: "success", text: "采集完成" });
  assert.equal(finishedQueueViews(next, next).length, 0);
});

test("已结束的站再开采集，旧终态不当完成，新 queueId 的终态要提示", () => {
  const starting = computeCollectQueueProgress(
    ["a"],
    [
      site("a", {
        id: "a",
        name: "a",
        total: 0,
        current: 0,
        success: 0,
        failed: 0,
        status: "starting",
        queueId: "q-local-new",
      }),
    ],
  );
  const staleDone = computeCollectQueueProgress(
    ["a"],
    [
      site("a", {
        id: "a",
        name: "a",
        total: 10,
        current: 10,
        success: 10,
        failed: 0,
        status: "done",
        queueId: "q-old",
      }),
    ],
  );
  const prev = [{ seq: 1, queueId: "q-local-new", staleQueueId: "q-old", view: starting! }];
  assert.equal(
    finishedQueueViews(prev, [{ seq: 1, queueId: "q-old", view: staleDone! }]).length,
    0,
  );
  assert.equal(finishedQueueViews(prev, []).length, 0);
  const fastDone = computeCollectQueueProgress(
    ["a"],
    [
      site("a", {
        id: "a",
        name: "a",
        total: 0,
        current: 0,
        success: 0,
        failed: 0,
        status: "done",
        queueId: "q-new",
      }),
    ],
  );
  assert.equal(
    finishedQueueViews(prev, [{ seq: 1, queueId: "q-new", view: fastDone! }]).length,
    1,
  );

  const localStart = {
    id: "a",
    name: "a",
    total: 0,
    current: 0,
    success: 0,
    failed: 0,
    status: "starting" as const,
    queueId: "q-local-new",
    staleQueueId: "q-old",
  };
  const kept = nextCollectProgress(localStart, {
    id: "a",
    name: "a",
    total: 10,
    current: 10,
    success: 10,
    failed: 0,
    status: "done",
    queueId: "q-old",
  });
  assert.equal(kept?.status, "starting");
  assert.equal(kept?.queueId, "q-local-new");
  assert.equal(nextCollectProgress(kept, undefined)?.queueId, "q-local-new");
  const failed = nextCollectProgress(localStart, {
    id: "a",
    name: "a",
    total: 0,
    current: 0,
    success: 0,
    failed: 0,
    status: "failed",
    queueId: "q-new",
  });
  assert.equal(failed?.status, "failed");
  assert.equal(failed?.queueId, "q-new");
});

test("isClientCollectQueueId 只认 q-local- 前缀", () => {
  assert.equal(isClientCollectQueueId("q-local-m1"), true);
  assert.equal(isClientCollectQueueId("q-batch-12"), false);
  assert.equal(isClientCollectQueueId(undefined), false);
});
