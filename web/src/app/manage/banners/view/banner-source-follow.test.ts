import assert from "node:assert/strict";
import { test } from "node:test";
import { shouldFollowPrimary } from "./banner-source-follow";

test("只在看着旧主站或还没选站点时跟随", () => {
  assert.equal(shouldFollowPrimary("", "old"), true);
  assert.equal(shouldFollowPrimary("old", "old"), true);
  assert.equal(shouldFollowPrimary("other", "old"), false);
  assert.equal(shouldFollowPrimary("other", ""), false);
});
