import { api } from "../api";
import { useLoad } from "../hooks";
import { primary } from "../strings";
import { Button, Card, LiveStatus } from "../components/ui";
import { AddressStep, CommandStep } from "./ServerSteps";
import { ReachCard } from "./ReachCard";

/** Reached from Home when the server still needs setting up or is not responding. */
export function ServerPage({ navigate }: { navigate: (to: string) => void }) {
  const s = primary.setup;
  const { data: net, reload } = useLoad(() => api.network());
  const { data: home } = useLoad(() => api.home(), 3000);

  if (!net || !home) return <p role="status">{primary.common.loading}</p>;
  const status = home.server.status;
  return (
    <div className="stack">
      <h1>{s.commandTitle}</h1>
      <Card>
        {!net.server_address ? (
          <>
            <h2>{s.addressTitle}</h2>
            <AddressStep onSaved={() => void reload()} />
          </>
        ) : status === "none" || status === "waiting" ? (
          <CommandStep onOnline={() => undefined} />
        ) : (
          <div className="stack">
            {status === "offline" && <p>{primary.home.serverOfflineBody}</p>}
            <LiveStatus done={status === "online"}>{status === "online" ? primary.home.serverOnline : primary.home.serverOffline}</LiveStatus>
            {status === "online" && <Button onClick={() => navigate("/")}>{s.goHome}</Button>}
          </div>
        )}
      </Card>
      {net.server_address && status === "online" && <ReachCard initial={!!net.devices_reach_each_other} />}
    </div>
  );
}
