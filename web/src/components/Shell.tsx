import { ReactNode } from "react";
import { primary } from "../strings";
import { Button } from "./ui";

export function Shell({ path, navigate, onSignOut, children }: { path: string; navigate: (to: string) => void; onSignOut: () => void; children: ReactNode }) {
  const link = (to: string, text: string) => (
    <a
      href={to}
      aria-current={path === to ? "page" : undefined}
      onClick={(e) => {
        e.preventDefault();
        navigate(to);
      }}
    >
      {text}
    </a>
  );
  return (
    <>
      <a className="skip" href="#main">
        {primary.common.skipToContent}
      </a>
      <header className="topbar">
        <a
          className="brand"
          href="/"
          onClick={(e) => {
            e.preventDefault();
            navigate("/");
          }}
        >
          <img src="/favicon.svg" alt="" width={32} height={32} />
          <span>{primary.common.appName}</span>
        </a>
        <nav aria-label={primary.common.mainNav}>
          {link("/", primary.nav.home)}
          {link("/activity", primary.nav.activity)}
        </nav>
        <Button variant="quiet" onClick={onSignOut}>
          {primary.common.signOut}
        </Button>
      </header>
      <main id="main" tabIndex={-1} className="page">
        {children}
      </main>
      {path !== "/add" && (
        <a
          className="fab"
          href="/add"
          onClick={(e) => {
            e.preventDefault();
            navigate("/add");
          }}
        >
          <span aria-hidden="true" className="fab-plus">+</span>
          <span>{primary.home.addDevice}</span>
        </a>
      )}
    </>
  );
}
