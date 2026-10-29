import { useState } from "react";
import { api } from "../api";
import { messageOf } from "../hooks";
import { primary } from "../strings";
import { Card } from "../components/ui";

/** The one network setting people ask for: can the devices reach each other. Off until they say so. */
export function ReachCard({ initial }: { initial: boolean }) {
  const s = primary.setup;
  const [on, setOn] = useState(initial);
  const [note, setNote] = useState<{ ok: boolean; text: string } | null>(null);
  async function change(next: boolean) {
    setOn(next);
    setNote(null);
    try {
      await api.setReach(next);
      setNote({ ok: true, text: s.reachSaved });
    } catch (err) {
      setOn(!next);
      setNote({ ok: false, text: messageOf(err) });
    }
  }
  return (
    <Card className="stack">
      <h2>{s.reachTitle}</h2>
      <p className="help">{s.reachHelp}</p>
      <label className="check">
        <input type="checkbox" checked={on} onChange={(e) => void change(e.target.checked)} />
        <span>{s.reachLabel}</span>
      </label>
      {note && <p className={note.ok ? "note" : "error"} role="status">{note.text}</p>}
    </Card>
  );
}
