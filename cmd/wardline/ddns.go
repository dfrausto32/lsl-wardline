package main

import (
	"context"
	"database/sql"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"lsl-wardline/internal/auth"
)

// ddnsUpdater keeps a free DuckDNS name pointed at this computer's current public address. DuckDNS
// takes the address from where the request comes from, so nothing has to be looked up first.
type ddnsUpdater struct {
	base     string // https://www.duckdns.org
	settings func(ctx context.Context) (name, token string, ok bool, err error)
	record   func(ctx context.Context, ip string, err error)
	every    time.Duration
	kick     chan struct{}
}

// once makes one update. Errors are recorded in plain words and never include the token.
func (u *ddnsUpdater) once(ctx context.Context) {
	name, token, ok, err := u.settings(ctx)
	if err != nil || !ok {
		return
	}
	q := url.Values{"domains": {name}, "token": {token}, "ip": {""}, "verbose": {"true"}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.base+"/update?"+q.Encode(), nil)
	if err != nil {
		u.record(ctx, "", errors.New("could not ask DuckDNS to update"))
		return
	}
	client := &http.Client{Timeout: 15 * time.Second}
	res, err := client.Do(req)
	if err != nil { // the real error carries the address with the token in it, so say it plainly instead
		u.record(ctx, "", errors.New("could not reach DuckDNS. Check that this computer is online"))
		return
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(res.Body, 512))
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if res.StatusCode != http.StatusOK || len(lines) == 0 || strings.TrimSpace(lines[0]) != "OK" {
		u.record(ctx, "", errors.New("DuckDNS did not accept the name or token. Check both on your duckdns.org page"))
		return
	}
	ip := ""
	if len(lines) > 1 {
		ip = strings.TrimSpace(lines[1])
	}
	u.record(ctx, ip, nil)
}

// run updates now, then regularly and whenever it is nudged, until ctx ends.
func (u *ddnsUpdater) run(ctx context.Context) {
	every := u.every
	if every == 0 {
		every = 5 * time.Minute
	}
	t := time.NewTicker(every)
	defer t.Stop()
	u.once(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			u.once(ctx)
		case <-u.kick:
			u.once(ctx)
		}
	}
}

// newDDNSUpdater reads the saved name and token from the database and writes the outcome back.
func newDDNSUpdater(db *sql.DB, box *auth.SecretBox) *ddnsUpdater {
	return &ddnsUpdater{
		base: "https://www.duckdns.org",
		kick: make(chan struct{}, 1),
		settings: func(ctx context.Context) (string, string, bool, error) {
			var name string
			var enc []byte
			err := db.QueryRowContext(ctx, `SELECT name, token_enc FROM dynamic_dns WHERE id=1`).Scan(&name, &enc)
			if errors.Is(err, sql.ErrNoRows) {
				return "", "", false, nil
			}
			if err != nil {
				return "", "", false, err
			}
			tok, err := box.OpenPacked(enc)
			if err != nil {
				return "", "", false, err
			}
			return name, string(tok), true, nil
		},
		record: func(ctx context.Context, ip string, err error) {
			now := time.Now().UTC().Format(time.RFC3339)
			if err != nil {
				db.ExecContext(ctx, `UPDATE dynamic_dns SET last_error=? WHERE id=1`, err.Error())
				return
			}
			db.ExecContext(ctx, `UPDATE dynamic_dns SET last_ok_at=?, last_ip=?, last_error=NULL WHERE id=1`, now, ip)
		},
	}
}

func (u *ddnsUpdater) nudge() {
	select {
	case u.kick <- struct{}{}:
	default:
	}
}
