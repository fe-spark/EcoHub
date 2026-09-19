import assert from "node:assert/strict";
import { test } from "node:test";
import {
  groupCollectQueues,
  isClientCollectQueueId,
  matchPrevCollectQueueBar,
  UNTAGGED_COLLECT_QUEUE_ID,
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
    grade: 1,
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
      status: "waiting_publish",
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

test("isClientCollectQueueId 只认 q-local- 前缀", () => {
  assert.equal(isClientCollectQueueId("q-local-m1"), true);
  assert.equal(isClientCollectQueueId("q-batch-12"), false);
  assert.equal(isClientCollectQueueId(UNTAGGED_COLLECT_QUEUE_ID), false);
});
