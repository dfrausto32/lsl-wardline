import { FormEvent, useState } from "react";
import { api } from "../api";
import { messageOf, useLoad } from "../hooks";
import { fmt, primary } from "../strings";
import { Button, Card, CopyBox, Field } from "../components/ui";

const e = primary.edge;

/** Advanced: make and remove the read-only keys the Edge uses to read the machine list. */
export function EdgeKeys() {
  const { data: keys, reload } = useLoad(() => api.tokens());
  const [name, setName] = useState("");
  const [shown, setShown] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function make(ev: FormEvent) {
    ev.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const made = await api.createToken(name.trim());
      setShown(made.token);
      setName("");
      void reload();
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  async function remove(id: string) {
    setError(null);
    try {
      await api.revokeToken(id);
      setShown(null);
      void reload();
    } catch (err) {
      setError(messageOf(err));
    }
  }

  return (
    <section role="region" aria-label={e.heading} className="stack">
      <h3>{e.heading}</h3>
      <p className="help">{e.help}</p>
      {shown && (
        <Card className="soft stack">
          <p>{e.shownOnce}</p>
          <CopyBox text={shown} label={e.keyLabel} />
        </Card>
      )}
      <form className="stack" onSubmit={make}>
        <Field label={e.nameLabel} maxLength={60} value={name} onChange={(ev) => setName(ev.target.value)} error={error} />
        <div className="actions">
          <Button type="submit" variant="secondary" busy={busy} disabled={name.trim() === ""}>{e.make}</Button>
        </div>
      </form>
      {keys && keys.length === 0 && <p className="help">{e.none}</p>}
      {keys && keys.length > 0 && (
        <ul className="devices">
          {keys.map((k) => (
            <li key={k.id} className="device-row">
              <span className="device-main">
                <span className="device-name">{k.name}</span>
              </span>
              <Button variant="quiet" onClick={() => void remove(k.id)} aria-label={fmt(e.removeNamed, { name: k.name })}>{e.remove}</Button>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
