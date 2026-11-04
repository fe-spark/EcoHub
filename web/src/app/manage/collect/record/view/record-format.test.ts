import assert from "node:assert/strict";
import { test } from "node:test";
import { formatCollectFilms, formatCollectPages, formatSourcePages, formatSourceWindow } from "./record-format";

test("停止不把没抓的页显示成失败", () => {
  const pages = formatCollectPages({ status: "stopped", pageSuccess: 3, pageTotal: 10, pageFailed: 0 });
  assert.equal(pages.primary, "成功 3 页");
  assert.equal(pages.secondary, "未抓 7 页");
  assert.equal(pages.alert, false);
});

test("一页没写出的停止不显示成全部拉完", () => {
  const pages = formatCollectPages({ status: "stopped", pageSuccess: 0, pageTotal: 8, pageFailed: 0 });
  assert.equal(pages.primary, "成功 0 页");
  assert.equal(pages.secondary, "未抓 8 页");
});

test("真实失败页和未抓页分开显示", () => {
  const pages = formatCollectPages({ status: "partial", pageSuccess: 4, pageTotal: 10, pageFailed: 2 });
  assert.equal(pages.primary, "2 页失败");
  assert.equal(pages.secondary, "成功 4 / 共 10 页，未抓 4 页");
  assert.equal(formatSourcePages({ status: "failed", pageTotal: 10, successCnt: 4, failedCnt: 2 }), "成功 4 / 共 10 页，失败 2 页，未抓 4 页");
});

test("成功且没有影片才写无变更", () => {
  assert.equal(formatCollectFilms(0, { status: "success" }), "无变更");
  assert.equal(formatCollectFilms(0, { status: "failed" }), "0 部");
  assert.equal(formatCollectFilms(0, { status: "stopped" }), "0 部");
  assert.equal(formatCollectFilms(2, { status: "success" }), "2 部");
});

test("站点窗口和批次不同才单独显示", () => {
  assert.equal(formatSourceWindow(48, 24), "48 小时");
  assert.equal(formatSourceWindow(24, 24), "");
  assert.equal(formatSourceWindow(-1, 24), "全量");
});
