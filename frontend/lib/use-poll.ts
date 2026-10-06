"use client";

import { useCallback, useEffect, useRef, useState } from "react";

// Loads data immediately and then every `intervalMs`, so changes made by the
// agent show up without a page refresh. Polling pauses while the tab is hidden.
export function usePoll<T>(load: () => Promise<T>, intervalMs = 3000) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(true);
  const mounted = useRef(true);

  const refresh = useCallback(async () => {
    try {
      const d = await load();
      if (!mounted.current) return;
      setData(d);
      setError(null);
    } catch (e) {
      if (mounted.current) setError(e instanceof Error ? e.message : String(e));
    } finally {
      if (mounted.current) setLoading(false);
    }
  }, [load]);

  useEffect(() => {
    mounted.current = true;
    // eslint-disable-next-line react-hooks/set-state-in-effect -- initial fetch
    refresh();
    const id = setInterval(() => {
      if (document.visibilityState === "visible") refresh();
    }, intervalMs);
    return () => {
      mounted.current = false;
      clearInterval(id);
    };
  }, [refresh, intervalMs]);

  return { data, error, loading, refresh };
}
