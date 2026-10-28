import { useCallback, useEffect, useState } from "react";

/** A tiny history-based router: paths like "/", "/add?person=ID", "/devices/ID". */
export function useRoute(): [string, (to: string) => void] {
  const [loc, setLoc] = useState(() => window.location.pathname + window.location.search);
  useEffect(() => {
    const on = () => setLoc(window.location.pathname + window.location.search);
    window.addEventListener("popstate", on);
    return () => window.removeEventListener("popstate", on);
  }, []);
  const navigate = useCallback((to: string) => {
    if (to !== window.location.pathname + window.location.search) window.history.pushState({}, "", to);
    setLoc(to);
    window.scrollTo(0, 0);
  }, []);
  return [loc, navigate];
}

export function parse(loc: string): { path: string; query: URLSearchParams } {
  const [path, search = ""] = loc.split("?");
  return { path: path || "/", query: new URLSearchParams(search) };
}
