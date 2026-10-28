import { ButtonHTMLAttributes, ReactNode, useEffect, useId, useRef, useState } from "react";
import type { DisplayStatus } from "../api";
import { primary } from "../strings";

export function Button({
  variant = "primary",
  busy,
  children,
  ...rest
}: ButtonHTMLAttributes<HTMLButtonElement> & { variant?: "primary" | "secondary" | "danger" | "quiet"; busy?: boolean }) {
  return (
    <button {...rest} disabled={rest.disabled || busy} aria-busy={busy || undefined} className={`btn btn-${variant} ${rest.className ?? ""}`}>
      {children}
    </button>
  );
}

export function LinkButton({ href, children, variant = "primary", onNavigate }: { href: string; children: ReactNode; variant?: "primary" | "secondary" | "quiet"; onNavigate: (to: string) => void }) {
  return (
    <a
      href={href}
      className={`btn btn-${variant}`}
      onClick={(e) => {
        if (e.metaKey || e.ctrlKey || e.shiftKey || e.button !== 0) return;
        e.preventDefault();
        onNavigate(href);
      }}
    >
      {children}
    </a>
  );
}

export function Field({
  label,
  help,
  error,
  ...rest
}: React.InputHTMLAttributes<HTMLInputElement> & { label: string; help?: string; error?: string | null }) {
  const id = useId();
  const helpId = `${id}-help`;
  const errId = `${id}-err`;
  return (
    <div className="field">
      <label htmlFor={id}>{label}</label>
      <input
        id={id}
        {...rest}
        aria-describedby={[help ? helpId : "", error ? errId : ""].filter(Boolean).join(" ") || undefined}
        aria-invalid={error ? true : undefined}
      />
      {help && (
        <p id={helpId} className="help">
          {help}
        </p>
      )}
      {error && (
        <p id={errId} className="error" role="alert">
          {error}
        </p>
      )}
    </div>
  );
}

export function Card({ children, className = "", ...rest }: { children: ReactNode; className?: string } & React.HTMLAttributes<HTMLElement>) {
  return (
    <section {...rest} className={`card ${className}`}>
      {children}
    </section>
  );
}

export function CheckIcon() {
  return (
    <svg className="icon" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true" focusable="false">
      <path d="M5 12.5l4.5 4.5L19 7.5" fill="none" stroke="currentColor" strokeWidth="3" strokeLinecap="round" strokeLinejoin="round" />
    </svg>
  );
}

function ClockIcon() {
  return (
    <svg className="icon" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true" focusable="false">
      <circle cx="12" cy="12" r="8.5" fill="none" stroke="currentColor" strokeWidth="2.5" />
      <path d="M12 7.5V12l3 2" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinecap="round" />
    </svg>
  );
}

function MoonIcon() {
  return (
    <svg className="icon" viewBox="0 0 24 24" width="20" height="20" aria-hidden="true" focusable="false">
      <path d="M19 14.5A7.5 7.5 0 0 1 9.5 5a7.5 7.5 0 1 0 9.5 9.5z" fill="none" stroke="currentColor" strokeWidth="2.5" strokeLinejoin="round" />
    </svg>
  );
}

/** Never colour alone: every status has an icon and a word. */
export function StatusChip({ status }: { status: DisplayStatus }) {
  const map = {
    connected: { text: primary.status.connected, icon: <CheckIcon />, cls: "chip-ok" },
    waiting: { text: primary.status.waiting, icon: <ClockIcon />, cls: "chip-wait" },
    not_seen: { text: primary.status.notSeen, icon: <MoonIcon />, cls: "chip-quiet" },
  }[status];
  return (
    <span className={`chip ${map.cls}`} data-status={status}>
      {map.icon}
      <span>{map.text}</span>
    </span>
  );
}

export function Spinner() {
  return <span className="spinner" aria-hidden="true" />;
}

/** A line of progress that announces itself to screen readers when it changes. */
export function LiveStatus({ done, children }: { done: boolean; children: ReactNode }) {
  return (
    <p className={`live ${done ? "live-done" : ""}`} role="status" aria-live="polite">
      {done ? <CheckIcon /> : <Spinner />}
      <span>{children}</span>
    </p>
  );
}

export function CopyBox({ text, label }: { text: string; label: string }) {
  const [copied, setCopied] = useState(false);
  return (
    <div className="copybox">
      <pre tabIndex={0} aria-label={label}>
        <code>{text}</code>
      </pre>
      <Button
        variant="secondary"
        onClick={async () => {
          try {
            await navigator.clipboard.writeText(text);
          } catch {
            const ta = document.createElement("textarea");
            ta.value = text;
            document.body.appendChild(ta);
            ta.select();
            document.execCommand("copy");
            ta.remove();
          }
          setCopied(true);
          setTimeout(() => setCopied(false), 2000);
        }}
      >
        {copied ? primary.common.copied : primary.common.copy}
      </Button>
    </div>
  );
}

export function Banner({ title, children, action }: { title: string; children: ReactNode; action?: ReactNode }) {
  return (
    <div className="banner" role="group" aria-label={title}>
      <div>
        <h3>{title}</h3>
        <p>{children}</p>
      </div>
      {action}
    </div>
  );
}

/** A confirmation that says what will happen. Uses the native dialog element for focus handling. */
export function ConfirmDialog({
  open,
  title,
  body,
  confirmLabel,
  cancelLabel,
  busy,
  error,
  onConfirm,
  onCancel,
}: {
  open: boolean;
  title: string;
  body: string;
  confirmLabel: string;
  cancelLabel: string;
  busy?: boolean;
  error?: string | null;
  onConfirm: () => void;
  onCancel: () => void;
}) {
  const ref = useRef<HTMLDialogElement>(null);
  const titleId = useId();
  useEffect(() => {
    const d = ref.current;
    if (!d) return;
    if (open && !d.open) d.showModal();
    if (!open && d.open) d.close();
  }, [open]);
  return (
    <dialog ref={ref} className="dialog" aria-labelledby={titleId} onCancel={(e) => { e.preventDefault(); onCancel(); }}>
      <h2 id={titleId}>{title}</h2>
      <p>{body}</p>
      {error && (
        <p className="error" role="alert">
          {error}
        </p>
      )}
      <div className="actions">
        <Button variant="secondary" onClick={onCancel}>
          {cancelLabel}
        </Button>
        <Button variant="danger" busy={busy} onClick={onConfirm}>
          {confirmLabel}
        </Button>
      </div>
    </dialog>
  );
}

export function PlatformIcon({ platform }: { platform: string }) {
  const paths: Record<string, ReactNode> = {
    ios: <><rect x="7" y="2.5" width="10" height="19" rx="2.5" /><path d="M10.5 18.5h3" /></>,
    android: <><rect x="7" y="2.5" width="10" height="19" rx="2.5" /><path d="M10.5 5.5h3" /></>,
    windows: <><rect x="3" y="4.5" width="18" height="12" rx="1.5" /><path d="M8 20h8M12 16.5V20" /></>,
    macos: <><rect x="3" y="4.5" width="18" height="12" rx="1.5" /><path d="M8 20h8M12 16.5V20" /></>,
    linux: <><rect x="3" y="4.5" width="18" height="12" rx="1.5" /><path d="M8 20h8M12 16.5V20" /></>,
  };
  return (
    <svg className="platform-icon" viewBox="0 0 24 24" width="28" height="28" aria-hidden="true" focusable="false" fill="none" stroke="currentColor" strokeWidth="1.8" strokeLinecap="round" strokeLinejoin="round">
      {paths[platform] ?? paths.windows}
    </svg>
  );
}
