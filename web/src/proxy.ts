import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

export default function proxy(request: NextRequest) {
  const { pathname, search } = request.nextUrl;

  if (pathname.startsWith("/manage")) {
    const authCookie = request.cookies.get("ecohub_auth_token");
    if (!authCookie?.value) {
      const loginUrl = new URL("/login", request.url);
      loginUrl.searchParams.set("redirect", pathname + search);
      const response = NextResponse.redirect(loginUrl);
      response.cookies.delete("ecohub_auth_token");
      return response;
    }
  }

  // 为下游服务端组件注入当前完整的路径与查询参数
  const requestHeaders = new Headers(request.headers);
  requestHeaders.set("x-current-path", pathname + search);

  return NextResponse.next({
    request: {
      headers: requestHeaders,
    },
  });
}

export const config = {
  matcher: ["/((?!_next/static|_next/image|favicon.ico).*)"],
};
