import { fmt, primary } from "./strings";

const s = primary.status;

/** "2 minutes ago" style text. */
export function relativeTime(iso: string | null | undefined, now = Date.now()): string {
  if (!iso) return s.never;
  const secs = Math.max(0, Math.round((now - new Date(iso).getTime()) / 1000));
  if (secs < 60) return s.justNow;
  const mins = Math.floor(secs / 60);
  if (mins < 60) return mins === 1 ? s.minuteAgo : fmt(s.minutesAgo, { n: mins });
  const hours = Math.floor(mins / 60);
  if (hours < 24) return hours === 1 ? s.hourAgo : fmt(s.hoursAgo, { n: hours });
  const days = Math.floor(hours / 24);
  return days === 1 ? s.dayAgo : fmt(s.daysAgo, { n: days });
}

/** The exact time, shown on hover. */
export function exactTime(iso: string | null | undefined): string {
  if (!iso) return "";
  return new Date(iso).toLocaleString(undefined, { dateStyle: "medium", timeStyle: "short" });
}

export function formatBytes(n: number): string {
  if (n < 1024) return `${n} B`;
  const units = ["KB", "MB", "GB", "TB"];
  let v = n / 1024;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(1)} ${units[i]}`;
}
