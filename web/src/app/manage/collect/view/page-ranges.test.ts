import assert from "node:assert/strict";
import { test } from "node:test";
import { countPages, formatPageRanges } from "./page-ranges";

test("页码按数值排序，连续页压成区间", () => {
  assert.equal(formatPageRanges([4765, 4762, 4764, 4763, 4801, 10, 9, 5000]), "9-10、4762-4765、4801、5000");
});

test("去重并忽略非正整数", () => {
  assert.equal(formatPageRanges([3, 3, 2, 0, -1, 2.5, 1]), "1-3");
  assert.equal(countPages([3, 3, 2, 0, -1, 2.5, 1]), 3);
});

test("空列表返回空串", () => {
  assert.equal(formatPageRanges([]), "");
  assert.equal(formatPageRanges(undefined), "");
  assert.equal(countPages(null), 0);
});

test("大段连续页只占一个区间", () => {
  const pages = Array.from({ length: 500 }, (_, i) => 4501 + i).reverse();
  assert.equal(formatPageRanges(pages), "4501-5000");
});
