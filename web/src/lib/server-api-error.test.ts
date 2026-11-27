import assert from "node:assert/strict";
import { test } from "node:test";
import {
  ServerApiError,
  buildHttpServerApiError,
  buildLoginRedirectTarget,
  getPublicServerErrorMessage,
  isSafeInternalRedirect,
  isServerUnauthorizedError,
} from "./server-api-error";

test("识别 HTTP 401 私有访问鉴权失败响应", () => {
  const err = buildHttpServerApiError(
    401,
    "Unauthorized",
    JSON.stringify({
      code: -1,
      data: { private_access: true },
      msg: "该站点已开启私有化访问，请登录账号或输入订阅密钥",
    }),
  );
  assert.equal(isServerUnauthorizedError(err), true);
  assert.equal(err.status, 401);
  assert.equal(err.code, -1);
  assert.deepEqual(err.payload, { private_access: true });
  assert.equal(err.message, "该站点已开启私有化访问，请登录账号或输入订阅密钥");
});

test("前台错误文案屏蔽内部 API_URL 与 5xx 原始错误", () => {
  const netErr = new ServerApiError({
    message: "服务端请求失败: http://127.0.0.1:8080/api/home; connect ECONNREFUSED",
    status: 0,
    isNetworkError: true,
  });
  assert.equal(isServerUnauthorizedError(netErr), false);
  assert.equal(getPublicServerErrorMessage(netErr), "无法连接后端服务，请稍后重试");

  const serverErr = new ServerApiError({
    message: "internal stack trace",
    status: 502,
  });
  assert.equal(getPublicServerErrorMessage(serverErr), "服务暂时不可用，请稍后重试");
});

test("登录跳转保留站内路径与参数并拦截非法跳转", () => {
  assert.equal(
    buildLoginRedirectTarget("/category?pid=17&sort=hits"),
    "/login?redirect=%2Fcategory%3Fpid%3D17%26sort%3Dhits",
  );
  assert.equal(buildLoginRedirectTarget("//evil.example/phish"), "/login");
  assert.equal(buildLoginRedirectTarget("/\\evil.example/phish"), "/login");
  assert.equal(buildLoginRedirectTarget("/login?redirect=/"), "/login");
  assert.equal(buildLoginRedirectTarget(null), "/login");

  assert.equal(isSafeInternalRedirect("/manage/film?tab=1"), true);
  assert.equal(isSafeInternalRedirect("//evil.example"), false);
  assert.equal(isSafeInternalRedirect("/\\evil.example"), false);
  assert.equal(isSafeInternalRedirect("/manage\r\nLocation: https://evil.example"), false);
  assert.equal(isSafeInternalRedirect("https://evil.example"), false);
});
