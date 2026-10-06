import type { ComposeDraft } from "./types";

const KEY = "insta-deploy-draft";

// Passes a prefilled Compose deployment to /deployments/new without putting
// secrets in the URL.
export function saveDraft(d: ComposeDraft) {
  sessionStorage.setItem(KEY, JSON.stringify(d));
}

// readDraft doesn't remove the draft: React may render (and discard) a
// component more than once before it mounts. Call clearDraft after mounting.
export function readDraft(): ComposeDraft | null {
  try {
    const raw = sessionStorage.getItem(KEY);
    return raw ? (JSON.parse(raw) as ComposeDraft) : null;
  } catch {
    return null;
  }
}

export function clearDraft() {
  try {
    sessionStorage.removeItem(KEY);
  } catch {}
}

// Random values for generated passwords and keys.
export function randomValue(kind: "password" | "secret"): string {
  const bytes = new Uint8Array(kind === "secret" ? 32 : 18);
  crypto.getRandomValues(bytes);
  if (kind === "secret") return Array.from(bytes, (b) => b.toString(16).padStart(2, "0")).join("");
  const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789";
  return Array.from(bytes, (b) => alphabet[b % alphabet.length]).join("");
}
