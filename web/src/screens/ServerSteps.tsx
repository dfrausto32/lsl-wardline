import { FormEvent, useEffect, useRef, useState } from "react";
import { NewServer, api } from "../api";
import { messageOf, useLoad } from "../hooks";
import { primary } from "../strings";
import { Button, CopyBox, Field, LiveStatus } from "../components/ui";

const looksLikeIp = (v: string) => /^[0-9.]+$/.test(v.trim()) || (v.includes(":") && /^[0-9a-fA-F:]+$/.test(v.trim()));

/** Asks for the name or address devices use to reach the server. */
export function AddressStep({ initial = "", onSaved }: { initial?: string; onSaved: () => void }) {
  const s = primary.setup;
  const [value, setValue] = useState(initial);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.setServerAddress(value.trim());
      onSaved();
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }
  const ip = looksLikeIp(value);
  return (
    <form onSubmit={submit} className="stack">
      <Field label={s.addressLabel} placeholder={s.addressPlaceholder} autoCapitalize="none" autoCorrect="off" spellCheck={false} required value={value} onChange={(e) => setValue(e.target.value)} help={s.addressHelp} error={error} />
      {ip ? <p className="note" role="note">{s.addressIpNote}</p> : <p className="help">{s.addressRecommend}</p>}
      <Button type="submit" busy={busy}>{primary.common.continue}</Button>
    </form>
  );
}

/** Shows the one command for the server and waits, live, for it to check in. */
export function CommandStep({ onOnline }: { onOnline: () => void }) {
  const s = primary.setup;
  const [created, setCreated] = useState<NewServer | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [tab, setTab] = useState<"docker" | "compose">("docker");
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    api.createServer().then(setCreated).catch((e) => setError(messageOf(e)));
  }, []);

  const { data: servers } = useLoad(() => api.servers(), 2000);
  const online = !!servers?.some((x) => x.status === "online");
  const fired = useRef(false);
  useEffect(() => {
    if (online && !fired.current) {
      fired.current = true;
      onOnline();
    }
  }, [online, onOnline]);

  if (error && !online) return <p className="error" role="alert">{error}</p>;
  return (
    <div className="stack">
      {created && !online && (
        <>
          <p>{s.commandHelp}</p>
          <div role="tablist" aria-label={s.commandTitle} className="tabs">
            <button role="tab" aria-selected={tab === "docker"} onClick={() => setTab("docker")}>{s.commandTab}</button>
            <button role="tab" aria-selected={tab === "compose"} onClick={() => setTab("compose")}>{s.composeTab}</button>
          </div>
          <CopyBox label={tab === "docker" ? s.commandTab : s.composeTab} text={tab === "docker" ? created.install.docker : created.install.compose} />
          <p className="help">{s.commandPort}</p>
        </>
      )}
      <LiveStatus done={online}>{online ? s.serverOnline : s.waiting}</LiveStatus>
    </div>
  );
}
