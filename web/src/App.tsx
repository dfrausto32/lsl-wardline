import { useCallback, useEffect, useRef, useState } from "react";
import { AppState, api, setSignedOutHandler } from "./api";
import { messageOf } from "./hooks";
import { parse, useRoute } from "./router";
import { primary } from "./strings";
import { Button } from "./components/ui";
import { Shell } from "./components/Shell";
import { SignIn } from "./screens/SignIn";
import { Setup } from "./screens/Setup";
import { Home } from "./screens/Home";
import { AddDevice } from "./screens/AddDevice";
import { DeviceDetails } from "./screens/DeviceDetails";
import { Activity } from "./screens/Activity";
import { ServerPage } from "./screens/ServerPage";

export default function App() {
  const [loc, navigate] = useRoute();
  const [state, setState] = useState<AppState | null>(null);
  const [error, setError] = useState<string | null>(null);
  const wizard = useRef<boolean | null>(null);
  const [, force] = useState(0);

  const refresh = useCallback(async () => {
    try {
      const s = await api.state();
      if (wizard.current === null) wizard.current = !s.setup_done;
      setState(s);
      setError(null);
    } catch (e) {
      setError(messageOf(e));
    }
  }, []);

  useEffect(() => {
    setSignedOutHandler(() => void refresh());
    void refresh();
  }, [refresh]);

  if (error && !state) {
    return (
      <main className="center">
        <p role="alert">{error}</p>
        <Button onClick={() => void refresh()}>{primary.common.tryAgain}</Button>
      </main>
    );
  }
  if (!state) return <main className="center"><p role="status">{primary.common.loading}</p></main>;

  if (wizard.current) {
    return (
      <Setup
        state={state}
        onStateChange={refresh}
        onFinish={() => {
          wizard.current = false;
          force((n) => n + 1);
          navigate("/");
        }}
      />
    );
  }
  if (!state.setup_done) return <Setup state={state} onStateChange={refresh} onFinish={() => void refresh()} />;
  if (!state.signed_in) return <SignIn onSignedIn={refresh} />;

  const { path, query } = parse(loc);
  let page;
  if (path === "/add") page = <AddDevice navigate={navigate} />;
  else if (path.startsWith("/devices/")) page = <DeviceDetails id={path.slice("/devices/".length)} navigate={navigate} />;
  else if (path === "/activity") page = <Activity initialDevice={query.get("device")} />;
  else if (path === "/server") page = <ServerPage navigate={navigate} />;
  else page = <Home navigate={navigate} removed={query.get("removed")} />;

  return (
    <Shell
      path={path}
      navigate={navigate}
      onSignOut={async () => {
        await api.logout().catch(() => undefined);
        await refresh();
      }}
    >
      {page}
    </Shell>
  );
}
