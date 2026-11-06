import assert from "node:assert/strict";
import { test } from "node:test";
import { resolveSourceStatus } from "./source-status";
import type { FilmSource } from "./types";

function source(partial: Partial<FilmSource>): FilmSource {
  return {
    id: "a",
    name: "HD",
    uri: "https://example.com",
    state: true,
    ...partial,
  };
}

test("采集中没有失败页时保持进行中色", () => {
  const status = resolveSourceStatus(
    source({ progress: { id: "a", name: "HD", total: 10, current: 2, success: 2, failed: 0, status: "running" } }),
    true,
  );
  assert.deepEqual(status, { label: "采集中", tone: "running" });
});

test("采集中有失败页时换成失败色，文案仍是采集中", () => {
  const status = resolveSourceStatus(
    source({
      progress: {
        id: "a",
        name: "HD",
        total: 3380,
        current: 372,
        success: 364,
        failed: 8,
        status: "running",
      },
    }),
    true,
  );
  assert.deepEqual(status, { label: "采集中", tone: "runningFailed" });
});
