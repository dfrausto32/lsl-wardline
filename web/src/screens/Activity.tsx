import { useEffect, useState } from "react";
import { api } from "../api";
import { exactTime, relativeTime } from "../format";
import { useLoad } from "../hooks";
import { primary } from "../strings";

const t = primary.activity;

export function Activity({ initialDevice }: { initialDevice: string | null }) {
  const [filter, setFilter] = useState<string>(initialDevice ? `device:${initialDevice}` : "");
  const { data: home } = useLoad(() => api.home());
  const params = filter.startsWith("device:") ? { device_id: filter.slice(7) } : {};
  const query = new URLSearchParams(params as Record<string, string>).toString();
  const { data: items, error, reload } = useLoad(() => api.activity(params), 4000);
  useEffect(() => {
    void reload();
  }, [filter, reload]);
  const devices = home?.devices ?? [];

  return (
    <div className="stack">
      <h1>{t.title}</h1>
      <div className="toolbar">
        <label className="inline-field">
          <span>{t.filterLabel}</span>
          <select value={filter} onChange={(e) => setFilter(e.target.value)}>
            <option value="">{t.everyone}</option>
            {devices.map((d) => (
              <option key={d.id} value={`device:${d.id}`}>{d.name}</option>
            ))}
          </select>
        </label>
        <a className="btn btn-secondary" href={`/api/activity.csv${query ? `?${query}` : ""}`} download="wardline-activity.csv">{t.exportCsv}</a>
      </div>

      {error && <p className="error" role="alert">{error}</p>}
      {items && items.length === 0 && <p className="empty">{t.empty}</p>}
      {items && items.length > 0 && (
        <ol className="timeline" key={filter}>
          {items.map((e) => (
            <li key={e.id}>
              <span>{e.text}</span>
              <time dateTime={e.at} title={exactTime(e.at)}>{relativeTime(e.at)}</time>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
