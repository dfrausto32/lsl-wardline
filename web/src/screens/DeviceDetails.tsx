import { FormEvent, useEffect, useState } from "react";
import { ApiError, Device, api } from "../api";
import { exactTime, formatBytes, relativeTime } from "../format";
import { messageOf, useLoad } from "../hooks";
import { advanced, fmt, primary } from "../strings";
import { Button, Card, ConfirmDialog, CopyBox, Field, PlatformIcon, StatusChip } from "../components/ui";

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
        </dl>
      </Card>

      <RenameCard id={dev.id} name={dev.name} />

      {dev.name_on_network && (
        <Card className="stack">
          <h2>{d.networkNameLabel}</h2>
          <CopyBox text={dev.name_on_network} label={d.networkNameLabel} />
          <p className="help">{d.networkNameHelp}</p>
          {dev.agent?.names === "unavailable" && <p className="note">{d.namesUnavailable}</p>}
        </Card>
      )}

      {dev.agent?.problem && (
        <Card className="soft">
          <h2>{primary.home.problemTitle.replace("{name}", dev.name)}</h2>
          <p>{primary.home.problem[dev.agent.problem] ?? primary.home.problemOther}</p>
        </Card>
      )}

      {dev.refresh_due && (
        <Card className="soft">
          <h2>{d.refreshTitle}</h2>
          <p>{d.refreshBody}</p>
          <Button variant="secondary" onClick={() => navigate("/add")}>{d.refreshButton}</Button>
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
            {dev.agent?.version && <div><dt>{advanced.agentVersion}</dt><dd>{dev.agent.version}</dd></div>}
            {adv.last_address_seen && <div><dt>{advanced.lastEndpoint}</dt><dd>{adv.last_address_seen}</dd></div>}
          </dl>
          <NamesEditor dev={dev} />
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

/** Change what a machine is called. */
function RenameCard({ id, name }: { id: string; name: string }) {
  const [value, setValue] = useState(name);
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  useEffect(() => setValue(name), [name]);
  async function save(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setNote(null);
    try {
      await api.renameDevice(id, value);
      setNote({ ok: true, text: d.renameSaved });
    } catch (err) {
      setNote({ ok: false, text: messageOf(err) });
    } finally {
      setBusy(false);
    }
  }
  return (
    <Card>
      <form className="stack" onSubmit={save}>
        <Field label={d.renameLabel} maxLength={60} value={value} onChange={(e) => setValue(e.target.value)} help={d.renameNamesNote} error={note && !note.ok ? note.text : null} />
        <div className="actions">
          <Button type="submit" variant="secondary" busy={busy} disabled={value.trim() === "" || value.trim() === name}>{d.renameSave}</Button>
        </div>
        {note?.ok && <p className="note" role="status">{note.text}</p>}
      </form>
    </Card>
  );
}

/** Advanced: set the short name by hand, and give the machine extra names. */
function NamesEditor({ dev }: { dev: Device }) {
  const n = primary.deviceNames;
  const [shortName, setShortName] = useState(dev.short_name_custom ? dev.short_name ?? "" : "");
  const [extra, setExtra] = useState((dev.aliases ?? []).join(", "));
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  const [busy, setBusy] = useState(false);
  // Follow the server only when the person is not in the middle of typing.
  const saved = `${dev.short_name_custom ? dev.short_name : ""}|${(dev.aliases ?? []).join(",")}`;
  useEffect(() => {
    setShortName(dev.short_name_custom ? dev.short_name ?? "" : "");
    setExtra((dev.aliases ?? []).join(", "));
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [dev.id, saved]);
  async function save(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setNote(null);
    try {
      const aliases = extra.split(",").map((s) => s.trim().toLowerCase()).filter(Boolean);
      await api.setDeviceNames(dev.id, { short_name: shortName.trim().toLowerCase(), aliases });
      setNote({ ok: true, text: n.saved });
    } catch (err) {
      setNote({ ok: false, text: messageOf(err) });
    } finally {
      setBusy(false);
    }
  }
  return (
    <form className="stack" style={{ marginTop: "1.5rem" }} onSubmit={save} aria-label={n.heading}>
      <h3>{n.heading}</h3>
      <Field label={n.shortName} help={n.shortNameHelp} placeholder={dev.short_name} maxLength={63} value={shortName} onChange={(e) => setShortName(e.target.value)} />
      <Field label={n.extraNames} help={n.extraNamesHelp} value={extra} onChange={(e) => setExtra(e.target.value)} error={note && !note.ok ? note.text : null} />
      <div className="actions">
        <Button type="submit" variant="secondary" busy={busy}>{n.save}</Button>
      </div>
      {note?.ok && <p className="note" role="status">{note.text}</p>}
    </form>
  );
}
