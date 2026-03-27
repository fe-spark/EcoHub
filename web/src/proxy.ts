import { NextResponse } from "next/server";
import type { NextRequest } from "next/server";

export default function proxy(request: NextRequest) {
  const { pathname } = request.nextUrl;

  if (pathname.startsWith("/manage")) {
    const authCookie = request.cookies.get("ecohub_auth_token");
    if (!authCookie?.value) {
      const response = NextResponse.redirect(new URL("/login", request.url));
      response.cookies.delete("ecohub_auth_token");
      return response;
    }
  }

  return NextResponse.next();
}

export const config = {
  matcher: ["/manage/:path*"],
};
