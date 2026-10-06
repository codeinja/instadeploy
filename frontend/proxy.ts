import { NextResponse, type NextRequest } from "next/server";
import { getSessionCookie } from "better-auth/cookies";

// Fast redirect for signed-out visitors. This only checks that a session
// cookie exists; the (app) layout and the Go backend validate the session
// for real.
export function proxy(request: NextRequest) {
  const signedIn = Boolean(getSessionCookie(request));
  const { pathname } = request.nextUrl;
  const isAuthPage = pathname === "/login" || pathname === "/register";

  if (!signedIn && !isAuthPage) {
    return NextResponse.redirect(new URL("/login", request.url));
  }
  return NextResponse.next();
}

export const config = {
  // Everything except API routes, Next internals and static files.
  matcher: ["/((?!api|_next/static|_next/image|favicon.ico|.*\\.[a-z]+$).*)"],
};
