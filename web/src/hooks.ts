import { useCallback, useEffect, useRef, useState } from "react";
import { ApiError } from "./api";
import { primary } from "./strings";

export function messageOf(e: unknown): string {
  if (e instanceof ApiError) return e.message || (e.status === 0 ? primary.common.cannotReach : primary.common.somethingWrong);
  return primary.common.somethingWrong;
}

/** Loads data once and again every `everyMs` (if given), so screens update without a refresh button. */
export function useLoad<T>(load: () => Promise<T>, everyMs?: number) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState<string | null>(null);
  const loadRef = useRef(load);
  loadRef.current = load;

  const reload = useCallback(async () => {
    try {
      setData(await loadRef.current());
      setError(null);
    } catch (e) {
      setError(messageOf(e));
    }
  }, []);

  useEffect(() => {
    let live = true;
    const run = () => {
      if (live) void reload();
    };
    run();
    if (!everyMs) return () => { live = false; };
    const t = setInterval(run, everyMs);
    return () => {
      live = false;
      clearInterval(t);
    };
  }, [reload, everyMs]);

  return { data, error, reload };
}
