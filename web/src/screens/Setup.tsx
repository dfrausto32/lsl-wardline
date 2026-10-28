import { FormEvent, useState } from "react";
import { AppState, TotpBegin, api } from "../api";
import { messageOf } from "../hooks";
import { fmt, primary } from "../strings";
import { Button, Card, Field } from "../components/ui";
import { AddressStep, CommandStep } from "./ServerSteps";

type Step = "welcome" | "signin" | "twostep" | "where" | "address" | "command";
const ORDER: Step[] = ["welcome", "signin", "twostep", "where", "address", "command"];

/** First-run setup, one screen at a time (UX flow 1). */
export function Setup({ state, onStateChange, onFinish }: { state: AppState; onStateChange: () => Promise<void>; onFinish: () => void }) {
  const s = primary.setup;
  const [step, setStep] = useState<Step>(state.setup_done ? "where" : "welcome");
  const [networkName, setNetworkName] = useState<string>(s.nameDefault);
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);
  const [totp, setTotp] = useState<TotpBegin | null>(null);
  const [code, setCode] = useState("");
  const [online, setOnline] = useState(false);

  const n = ORDER.indexOf(step) + 1;

  async function createSignIn(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.setup({ network_name: networkName, email, password });
      await onStateChange();
      setStep("twostep");
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  async function beginTotp() {
    setBusy(true);
    setError(null);
    try {
      setTotp(await api.totpBegin());
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  async function confirmTotp(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      await api.totpConfirm(code.trim());
      setStep("where");
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="narrow" id="main">
      <p className="progress">{fmt(s.progress, { n, total: ORDER.length })}</p>

      {step === "welcome" && (
        <>
          <h1>{s.welcomeTitle}</h1>
          <p className="lead">{s.welcomeBody}</p>
          <Card>
            <form className="stack" onSubmit={(e) => { e.preventDefault(); setStep("signin"); }}>
              <Field label={s.nameLabel} help={s.nameHelp} required maxLength={60} value={networkName} onChange={(e) => setNetworkName(e.target.value)} />
              <Button type="submit">{primary.common.continue}</Button>
            </form>
          </Card>
        </>
      )}

      {step === "signin" && (
        <>
          <h1>{s.signInTitle}</h1>
          <p className="lead">{s.signInHelp}</p>
          <Card>
            <form className="stack" onSubmit={createSignIn}>
              <Field label={s.email} type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
              <Field label={s.password} type="password" autoComplete="new-password" required minLength={10} help={s.passwordHelp} value={password} onChange={(e) => setPassword(e.target.value)} />
              {error && <p className="error" role="alert">{error}</p>}
              <Button type="submit" busy={busy}>{s.signInButton}</Button>
              <Button type="button" variant="quiet" onClick={() => setStep("welcome")}>{primary.common.back}</Button>
            </form>
          </Card>
        </>
      )}

      {step === "twostep" && (
        <>
          <h1>{s.twoStepTitle}</h1>
          <Card>
            {!totp ? (
              <div className="stack">
                <p>{s.twoStepHelp}</p>
                {error && <p className="error" role="alert">{error}</p>}
                <Button busy={busy} onClick={beginTotp}>{s.twoStepSetUp}</Button>
                <Button variant="quiet" onClick={() => setStep("where")}>{s.twoStepSkip}</Button>
              </div>
            ) : (
              <form className="stack" onSubmit={confirmTotp}>
                <p>{s.twoStepScan}</p>
                <img className="qr" alt={s.twoStepScan} width={240} height={240} src={`data:image/png;base64,${totp.qr_png_base64}`} />
                <p className="help">{s.twoStepType} <code>{totp.secret}</code></p>
                <Field label={s.twoStepEnter} inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} required value={code} onChange={(e) => setCode(e.target.value)} error={error} />
                <Button type="submit" busy={busy}>{s.twoStepConfirm}</Button>
                <Button type="button" variant="quiet" onClick={() => setStep("where")}>{s.twoStepSkip}</Button>
              </form>
            )}
          </Card>
        </>
      )}

      {step === "where" && (
        <>
          <h1>{s.whereTitle}</h1>
          <p className="lead">{s.whereHelp}</p>
          <div className="choices">
            <button className="choice" onClick={() => setStep("address")}>
              <strong>{s.whereServer}</strong>
              <span>{s.whereServerHelp}</span>
            </button>
            <button className="choice" onClick={() => setStep("address")}>
              <strong>{s.whereHere}</strong>
              <span>{s.whereHereHelp}</span>
            </button>
            <button className="choice" onClick={onFinish}>
              <strong>{s.whereLater}</strong>
              <span>{s.whereLaterHelp}</span>
            </button>
          </div>
        </>
      )}

      {step === "address" && (
        <>
          <h1>{s.addressTitle}</h1>
          <Card>
            <AddressStep onSaved={() => setStep("command")} />
          </Card>
        </>
      )}

      {step === "command" && (
        <>
          <h1>{s.commandTitle}</h1>
          <Card>
            <CommandStep onOnline={() => setOnline(true)} />
          </Card>
          <div className="actions">
            {online ? <Button onClick={onFinish}>{s.goHome}</Button> : <Button variant="quiet" onClick={onFinish}>{s.later}</Button>}
          </div>
        </>
      )}
    </main>
  );
}
