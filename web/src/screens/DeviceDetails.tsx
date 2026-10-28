import { useState } from "react";
import { ApiError, api } from "../api";
import { exactTime, formatBytes, relativeTime } from "../format";
import { messageOf, useLoad } from "../hooks";
import { advanced, fmt, primary } from "../strings";
import { Button, Card, ConfirmDialog, PlatformIcon, StatusChip } from "../components/ui";

const d = primary.device;

export function DeviceDetails({ id, navigate }: { id: string; navigate: (to: string) => void }) {
  const { data: dev, error } = useLoad(() => api.device(id), 3000);
  const [confirming, setConfirming] = useState(false);
  const [busy, setBusy] = useState(false);
  const [removeError, setRemoveError] = useState<string | null>(null);

  if (error && !dev) {
    return (
      <div className="stack">
        <h1>{d.notFound}</h1>
        <Button onClick={() => navigate("/")}>{d.backHome}</Button>
      </div>
    );
  }
  if (!dev) return <p role="status">{primary.common.loading}</p>;

  async function remove() {
    if (!dev) return;
    setBusy(true);
    setRemoveError(null);
    try {
      await api.removeDevice(dev.id);
      navigate(`/?removed=${encodeURIComponent(dev.name)}`);
    } catch (e) {
      if (e instanceof ApiError && e.status === 404) navigate("/");
      else setRemoveError(messageOf(e));
    } finally {
      setBusy(false);
    }
  }

  const adv = dev.advanced;
  return (
    <div className="stack">
      <div className="device-head">
        <PlatformIcon platform={dev.platform} />
        <h1>{dev.name}</h1>
      </div>

      <Card>
        <dl className="facts">
          <div>
            <dt>{d.status}</dt>
            <dd><StatusChip status={dev.status} /></dd>
          </div>
          <div>
            <dt>{d.lastSeen}</dt>
            <dd title={exactTime(dev.last_seen_at)}>{relativeTime(dev.last_seen_at)}</dd>
          </div>
          <div>
            <dt>{d.added}</dt>
            <dd title={exactTime(dev.added_at)}>{relativeTime(dev.added_at)}</dd>
          </div>
          <div>
            <dt>{d.owner}</dt>
            <dd>{dev.person_name}</dd>
          </div>
        </dl>
      </Card>

      {dev.refresh_due && (
        <Card className="soft">
          <h2>{d.refreshTitle}</h2>
          <p>{d.refreshBody}</p>
          <Button variant="secondary" onClick={() => navigate(`/add?person=${dev.person_id}`)}>{d.refreshButton}</Button>
        </Card>
      )}

      <div className="actions">
        <Button variant="danger" onClick={() => setConfirming(true)}>{d.removeAccess}</Button>
        <Button variant="quiet" onClick={() => navigate(`/activity?device=${dev.id}`)}>{primary.nav.activity}</Button>
      </div>

      {adv && (
        <details className="advanced">
          <summary>{advanced.heading}</summary>
          <dl className="facts">
            <div><dt>{advanced.publicKey}</dt><dd><code>{adv.public_key}</code></dd></div>
            <div><dt>{advanced.address}</dt><dd>{adv.address}</dd></div>
            <div><dt>{advanced.received}</dt><dd>{formatBytes(adv.received_bytes)}</dd></div>
            <div><dt>{advanced.sent}</dt><dd>{formatBytes(adv.sent_bytes)}</dd></div>
            {adv.last_address_seen && <div><dt>{advanced.lastEndpoint}</dt><dd>{adv.last_address_seen}</dd></div>}
          </dl>
        </details>
      )}

      <ConfirmDialog
        open={confirming}
        title={fmt(d.removeTitle, { name: dev.name })}
        body={d.removeBody}
        confirmLabel={d.removeConfirm}
        cancelLabel={d.keep}
        busy={busy}
        error={removeError}
        onConfirm={remove}
        onCancel={() => setConfirming(false)}
      />
    </div>
  );
}
