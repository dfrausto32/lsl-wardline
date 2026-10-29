import { FormEvent, useEffect, useRef, useState } from "react";
import { AddedAgentDevice, AddedDevice, ApiError, Platform, api } from "../api";
import { messageOf, useLoad } from "../hooks";
import { fmt, primary } from "../strings";
import { Button, Card, CopyBox, Field, LiveStatus, PlatformIcon } from "../components/ui";

const a = primary.add;
const PLATFORMS: Platform[] = ["ios", "android", "windows", "macos", "linux"];
const isPhone = (p: Platform) => p === "ios" || p === "android";
// Windows and Linux computers set themselves up with the Wardline agent; the rest use the WireGuard app.
const usesAgent = (p: Platform) => p === "windows" || p === "linux";
// Always the newest published release of the program.
const RELEASES = "https://github.com/dfrausto32/lsl-wardline/releases/latest/download";
const RELEASE_WINDOWS = `${RELEASES}/wardline.exe`;
const RELEASE_LINUX = `${RELEASES}/wardline-linux-install.tar.gz`;

function downloadFile(filename: string, text: string) {
  const url = URL.createObjectURL(new Blob([text], { type: "text/plain" }));
  const link = document.createElement("a");
  link.href = url;
  link.download = filename;
  document.body.appendChild(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
}

/** Add a machine (UX flow 2): what kind, what to call it, then the code or file and a live check. */
export function AddDevice({ navigate }: { navigate: (to: string) => void }) {
  const [platform, setPlatform] = useState<Platform | null>(null);
  const [name, setName] = useState<string | null>(null);

  if (!platform) {
    return (
      <div className="stack">
        <p className="progress">{fmt(a.stepOf, { n: 1, total: 3 })}</p>
        <h1>{a.kindTitle}</h1>
        <div className="kinds">
          {PLATFORMS.map((p) => (
            <button key={p} className="kind" onClick={() => setPlatform(p)}>
              <PlatformIcon platform={p} />
              <span>{primary.platform[p]}</span>
            </button>
          ))}
        </div>
      </div>
    );
  }
  if (name === null) {
    return <NameStep platform={platform} onPick={setName} onBack={() => setPlatform(null)} />;
  }
  const again = () => { setPlatform(null); setName(null); };
  return usesAgent(platform)
    ? <ShowAgentSetup name={name} platform={platform} navigate={navigate} onAnother={again} />
    : <ShowCode name={name} platform={platform} navigate={navigate} onAnother={again} />;
}

/** What to call it. Left empty, it is named after its kind, such as "Windows computer". */
function NameStep({ platform, onPick, onBack }: { platform: Platform; onPick: (name: string) => void; onBack: () => void }) {
  const [value, setValue] = useState("");
  function next(e: FormEvent) {
    e.preventDefault();
    onPick(value.trim());
  }
  return (
    <div className="stack">
      <p className="progress">{fmt(a.stepOf, { n: 2, total: 3 })}</p>
      <h1>{a.nameTitle}</h1>
      <Card>
        <form className="stack" onSubmit={next}>
          <Field label={a.nameLabel} help={a.nameHelp} placeholder={primary.platform[platform]} maxLength={60} autoFocus value={value} onChange={(e) => setValue(e.target.value)} />
          <Button type="submit">{primary.common.continue}</Button>
          <Button type="button" variant="quiet" onClick={onBack}>{primary.common.back}</Button>
        </form>
      </Card>
    </div>
  );
}

/** A computer that sets itself up: one command, then a live check. The key is made on the computer and never seen here. */
function ShowAgentSetup({ name: chosen, platform, navigate, onAnother }: { name: string; platform: Platform; navigate: (to: string) => void; onAnother: () => void }) {
  const [added, setAdded] = useState<AddedAgentDevice | null>(null);
  const [error, setError] = useState<{ message: string; notReady: boolean } | null>(null);
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    api
      .addAgentDevice({ platform, name: chosen || undefined })
      .then(setAdded)
      .catch((e) => setError({ message: messageOf(e), notReady: e instanceof ApiError && e.status === 409 }));
  }, [chosen, platform]);

  const id = added?.device.id;
  const { data: live } = useLoad(() => api.device(id as string), id ? 2000 : undefined);
  const connected = live?.status === "connected";

  if (error) {
    return (
      <div className="stack">
        <h1>{error.notReady ? a.needServerTitle : primary.common.somethingWrong}</h1>
        <Card>
          <p role="alert">{error.message}</p>
          {error.notReady && <Button onClick={() => navigate("/server")}>{a.needServerButton}</Button>}
        </Card>
      </div>
    );
  }
  if (!added) return <p role="status">{primary.common.loading}</p>;

  const name = added.device.name;
  return (
    <div className="stack">
      <p className="progress">{fmt(a.stepOf, { n: 3, total: 3 })}</p>
      <h1>{name}</h1>
      <Card className="stack">
        <h2>{a.getTitle}</h2>
        <a className="btn btn-secondary" href={platform === "windows" ? RELEASE_WINDOWS : RELEASE_LINUX}>
          {platform === "windows" ? a.getWindowsButton : a.getLinuxButton}
        </a>
        <p className="note">{platform === "windows" ? a.getWindowsHelp : a.getLinuxHelp}</p>
      </Card>
      <Card className="stack">
        <h2>{a.agentTitle}</h2>
        <p>{platform === "windows" ? a.agentWindowsStep : a.agentLinuxStep}</p>
        <CopyBox label={a.agentCommandLabel} text={added.install.command} />
        <p className="note">{a.agentOnce}</p>
      </Card>
      <LiveStatus done={connected}>{connected ? fmt(a.connected, { name }) : fmt(a.waiting, { name })}</LiveStatus>
      {!connected && <p className="help">{a.agentWaitingHelp}</p>}
      <div className="actions">
        {connected ? (
          <>
            <Button onClick={() => navigate("/")}>{primary.common.done}</Button>
            <Button variant="secondary" onClick={onAnother}>{a.addAnother}</Button>
          </>
        ) : (
          <Button variant="quiet" onClick={() => navigate("/")}>{primary.common.done}</Button>
        )}
      </div>
    </div>
  );
}

function ShowCode({ name: chosen, platform, navigate, onAnother }: { name: string; platform: Platform; navigate: (to: string) => void; onAnother: () => void }) {
  const [added, setAdded] = useState<AddedDevice | null>(null);
  const [error, setError] = useState<{ message: string; notReady: boolean } | null>(null);
  const started = useRef(false);

  useEffect(() => {
    if (started.current) return;
    started.current = true;
    api
      .addDevice({ platform, name: chosen || undefined })
      .then(setAdded)
      .catch((e) => setError({ message: messageOf(e), notReady: e instanceof ApiError && e.status === 409 }));
  }, [chosen, platform]);

  const id = added?.device.id;
  const { data: live } = useLoad(() => api.device(id as string), id ? 2000 : undefined);
  const connected = live?.status === "connected";

  if (error) {
    return (
      <div className="stack">
        <h1>{error.notReady ? a.needServerTitle : primary.common.somethingWrong}</h1>
        <Card>
          <p role="alert">{error.message}</p>
          {error.notReady && <Button onClick={() => navigate("/server")}>{a.needServerButton}</Button>}
        </Card>
      </div>
    );
  }
  if (!added) return <p role="status">{primary.common.loading}</p>;

  const name = added.device.name;
  return (
    <div className="stack">
      <p className="progress">{fmt(a.stepOf, { n: 3, total: 3 })}</p>
      <h1>{name}</h1>
      <Card className="stack">
        <h2>{a.installTitle}</h2>
        <a className="btn btn-secondary" href={added.app_link} target="_blank" rel="noreferrer noopener">{a.installButton}</a>
      </Card>

      {isPhone(platform) ? (
        <Card className="stack">
          <h2>{a.scanTitle}</h2>
          <p>{a.phoneStep}</p>
          <img className="qr" alt={a.scanAlt} width={280} height={280} src={`data:image/png;base64,${added.qr_png_base64}`} />
          <p className="note">{a.onceCode}</p>
        </Card>
      ) : (
        <Card className="stack">
          <h2>{a.downloadTitle}</h2>
          <p>{a.computerStep}</p>
          <Button onClick={() => downloadFile(added.filename, added.config)}>{a.downloadButton}</Button>
          <p className="note">{a.onceFile}</p>
        </Card>
      )}

      <LiveStatus done={connected}>{connected ? fmt(a.connected, { name }) : fmt(a.waiting, { name })}</LiveStatus>
      {!connected && <p className="help">{a.waitingHelp}</p>}

      <div className="actions">
        {connected ? (
          <>
            <Button onClick={() => navigate("/")}>{primary.common.done}</Button>
            <Button variant="secondary" onClick={onAnother}>{a.addAnother}</Button>
          </>
        ) : (
          <Button variant="quiet" onClick={() => navigate("/")}>{primary.common.done}</Button>
        )}
      </div>
    </div>
  );
}
