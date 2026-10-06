export class ApiError extends Error {
  constructor(
    public status: number,
    message: string,
  ) {
    super(message);
  }
}

// Calls the Go backend through the /api proxy. The Better Auth session
// cookie is sent automatically.
export async function api<T>(path: string, init: { method?: string; body?: unknown } = {}): Promise<T> {
  const isForm = typeof FormData !== "undefined" && init.body instanceof FormData;
  const res = await fetch(`/api${path}`, {
    method: init.method ?? "GET",
    headers: init.body !== undefined && !isForm ? { "Content-Type": "application/json" } : undefined,
    body: init.body === undefined ? undefined : isForm ? (init.body as FormData) : JSON.stringify(init.body),
  });

  if (res.status === 401) {
    // Session expired or revoked. A full page load also clears client state.
    // eslint-disable-next-line @next/next/no-location-assign-relative-destination
    window.location.assign("/login");
    throw new ApiError(401, "Your session has expired. Please sign in again.");
  }
  if (res.status === 204 || res.status === 202) return undefined as T;

  const data = await res.json().catch(() => ({}));
  if (!res.ok) throw new ApiError(res.status, data.error ?? `Request failed (${res.status})`);
  return data as T;
}

export function errorMessage(e: unknown): string {
  return e instanceof Error ? e.message : String(e);
}
