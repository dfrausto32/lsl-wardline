import { FormEvent, useState } from "react";
import { api } from "../api";
import { messageOf } from "../hooks";
import { primary } from "../strings";
import { Button, Card, Field } from "../components/ui";

export function SignIn({ onSignedIn }: { onSignedIn: () => void }) {
  const s = primary.signIn;
  const [email, setEmail] = useState("");
  const [password, setPassword] = useState("");
  const [code, setCode] = useState("");
  const [needCode, setNeedCode] = useState(false);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  async function submit(e: FormEvent) {
    e.preventDefault();
    setBusy(true);
    setError(null);
    try {
      const r = await api.login({ email, password, code: needCode ? code : undefined });
      if (r.need_code) setNeedCode(true);
      else onSignedIn();
    } catch (err) {
      setError(messageOf(err));
    } finally {
      setBusy(false);
    }
  }

  return (
    <main className="narrow" id="main">
      <h1>{needCode ? s.codeTitle : s.title}</h1>
      <Card>
        <form onSubmit={submit} className="stack">
          {!needCode && (
            <>
              <Field label={s.email} type="email" autoComplete="username" required value={email} onChange={(e) => setEmail(e.target.value)} />
              <Field label={s.password} type="password" autoComplete="current-password" required value={password} onChange={(e) => setPassword(e.target.value)} />
            </>
          )}
          {needCode && (
            <Field label={s.codeLabel} help={s.codeHelp} inputMode="numeric" autoComplete="one-time-code" pattern="[0-9]{6}" maxLength={6} required autoFocus value={code} onChange={(e) => setCode(e.target.value)} />
          )}
          {error && <p className="error" role="alert">{error}</p>}
          <Button type="submit" busy={busy}>{s.button}</Button>
        </form>
      </Card>
    </main>
  );
}
