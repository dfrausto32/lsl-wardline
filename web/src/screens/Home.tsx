import { useState } from "react";
import { Attention, Device, api } from "../api";
import { exactTime, relativeTime } from "../format";
import { useLoad } from "../hooks";
import { fmt, primary } from "../strings";
import { Banner, Button, Card, PlatformIcon, StatusChip } from "../components/ui";

const h = primary.home;

function DeviceRow({ d, onOpen }: { d: Device; onOpen: () => void }) {
  return (
    <li>
      <a
        className="device-row"
        href={`/devices/${d.id}`}
        onClick={(e) => {
          e.preventDefault();
          onOpen();
        }}
      >
        <PlatformIcon platform={d.platform} />
        <span className="device-main">
          <span className="device-name">{d.name}</span>
          <span className="device-sub" title={exactTime(d.last_seen_at)}>
            {d.last_seen_at ? fmt(primary.status.lastSeen, { when: relativeTime(d.last_seen_at) }) : primary.status.never}
          </span>
        </span>
        <StatusChip status={d.status} />
      </a>
    </li>
  );
}

function AttentionBanner({ a, navigate }: { a: Attention; navigate: (to: string) => void }) {
  if (a.kind === "server_offline") {
    return (
      <Banner title={h.serverOfflineTitle} action={<Button onClick={() => navigate("/server")}>{h.serverOfflineButton}</Button>}>
        {h.serverOfflineBody}
      </Banner>
    );
  }
  if (a.kind === "device_unseen_30_days") {
    return (
      <Banner title={fmt(h.unseenTitle, { name: a.device_name })} action={<Button onClick={() => navigate(`/devices/${a.device_id}`)}>{h.unseenButton}</Button>}>
        {h.unseenBody}
      </Banner>
    );
  }
  if (a.kind === "refresh_failed") {
    return (
      <Banner title={fmt(h.refreshFailedTitle, { name: a.device_name })} action={<Button variant="secondary" onClick={() => navigate(`/devices/${a.device_id}`)}>{h.refreshFailedButton}</Button>}>
        {h.refreshFailedBody}
      </Banner>
    );
  }
  if (a.kind === "device_problem") {
    return (
      <Banner title={fmt(h.problemTitle, { name: a.device_name })} action={<Button variant="secondary" onClick={() => navigate(`/devices/${a.device_id}`)}>{h.problemButton}</Button>}>
        {h.problem[a.problem] ?? h.problemOther}
      </Banner>
    );
  }
  return (
    <Banner title={fmt(h.refreshTitle, { name: a.device_name })} action={<Button variant="secondary" onClick={() => navigate(`/devices/${a.device_id}`)}>{h.refreshButton}</Button>}>
      {h.refreshBody}
    </Banner>
  );
}

export function Home({ navigate, removed }: { navigate: (to: string) => void; removed: string | null }) {
  const { data: home, error } = useLoad(() => api.home(), 3000);
  const [flashHidden, setFlashHidden] = useState(false);

  if (error && !home) return <p className="error" role="alert">{error}</p>;
  if (!home) return <p role="status">{primary.common.loading}</p>;

  const deviceCount = home.devices.length;
  const serverReady = home.server.status === "online" || home.server.status === "offline";
  const serverText = { online: h.serverOnline, offline: h.serverOffline, waiting: h.serverWaiting, none: h.serverNone }[home.server.status];

  return (
    <div className="stack">
      <h1>{home.network.name}</h1>

      {removed && !flashHidden && (
        <p className="flash" role="status">
          {fmt(primary.device.removed, { name: removed })}
          <button className="link" onClick={() => setFlashHidden(true)}>{primary.common.done}</button>
        </p>
      )}

      {home.attention.length > 0 && (
        <div className="stack" role="region" aria-label={h.attentionRegion}>
          {home.attention.map((a, i) => (
            <AttentionBanner key={i} a={a} navigate={navigate} />
          ))}
        </div>
      )}

      {!serverReady && home.server.status !== "offline" && (
        <Card className="cta">
          <h2>{h.finishServerTitle}</h2>
          <p>{h.finishServerBody}</p>
          <Button onClick={() => navigate("/server")}>{h.finishServerButton}</Button>
        </Card>
      )}

      {serverReady && deviceCount === 0 && (
        <Card className="cta">
          <h2>{h.firstCardTitle}</h2>
          <p>{h.firstCardBody}</p>
          <Button onClick={() => navigate("/add")}>{h.addDevice}</Button>
        </Card>
      )}

      {deviceCount > 0 && (
        <section aria-labelledby="machines-h" className="stack">
          <h2 id="machines-h">{h.machinesHeading}</h2>
          <Card>
            <ul className="devices">
              {home.devices.map((d) => (
                <DeviceRow key={d.id} d={d} onOpen={() => navigate(`/devices/${d.id}`)} />
              ))}
            </ul>
          </Card>
        </section>
      )}

      <footer className="server-status" role="status" data-server={home.server.status}>
        {serverText}
      </footer>
    </div>
  );
}
