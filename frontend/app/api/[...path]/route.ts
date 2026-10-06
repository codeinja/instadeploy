// Forwards /api/* from the browser to the Go backend (except /api/auth/*,
// which Better Auth handles in app/api/auth). The backend URL is read at
// runtime (BACKEND_URL), and the browser never talks to the backend
// directly, so there's no CORS to configure.

const BACKEND_URL = process.env.BACKEND_URL ?? "http://localhost:8080";

// Cookies are SameSite=Lax, which already stops most cross-site requests.
// As a second layer, reject state-changing requests from other origins.
function isCrossSite(request: Request): boolean {
  if (["GET", "HEAD", "OPTIONS"].includes(request.method)) return false;
  const fetchSite = request.headers.get("sec-fetch-site");
  if (fetchSite) return fetchSite !== "same-origin" && fetchSite !== "none";
  const origin = request.headers.get("origin");
  if (!origin) return false; // non-browser client
  const host =
    request.headers.get("x-forwarded-host") ?? request.headers.get("host");
  return new URL(origin).host !== host;
}

async function proxy(request: Request, ctx: RouteContext<"/api/[...path]">) {
  if (isCrossSite(request)) {
    return Response.json(
      { error: "cross-site request blocked" },
      { status: 403 },
    );
  }

  const { path } = await ctx.params;
  const url = new URL(request.url);
  const target = `${BACKEND_URL}/api/${path.map(encodeURIComponent).join("/")}${url.search}`;

  const headers = new Headers();
  for (const name of [
    "authorization",
    "content-type",
    "cookie",
    "accept",
    "last-event-id",
  ]) {
    const value = request.headers.get(name);
    if (value) headers.set(name, value);
  }

  try {
    const hasBody = !["GET", "HEAD"].includes(request.method);
    const res = await fetch(target, {
      method: request.method,
      headers,
      // Stream the body so large uploads (build contexts) aren't buffered.
      body: hasBody ? request.body : undefined,
      // @ts-expect-error -- required by Node's fetch when streaming a body
      duplex: hasBody ? "half" : undefined,
      cache: "no-store",
      signal: request.signal,
    });
    const out = new Headers();
    for (const name of [
      "content-type",
      "content-disposition",
      "cache-control",
    ]) {
      const value = res.headers.get(name);
      if (value) out.set(name, value);
    }
    return new Response(res.body, { status: res.status, headers: out });
  } catch (e) {
    if (request.signal.aborted) return new Response(null, { status: 499 });
    console.error("backend proxy error:", e);
    return Response.json(
      { error: "Cannot reach the Insta Deploy backend. Is it running?" },
      { status: 502 },
    );
  }
}

export {
  proxy as GET,
  proxy as POST,
  proxy as PUT,
  proxy as PATCH,
  proxy as DELETE,
};
