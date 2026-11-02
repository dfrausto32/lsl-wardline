package api

import (
	"context"
	"database/sql"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"lsl-wardline/internal/membership"
)

// A managed group records its membership in the signed log (specs/MEMBERSHIP-AND-MODES.md section 5.4).
// The devices and servers tables stay the primary data; the log is the record, written by the control plane
// as the authority. reconcileGroup compares the two and writes whatever entries are missing, so it can be
// called after any change, and again, safely.

const authorityName = "Wardline control plane"

type desiredMember struct {
	id, name, label, key, address string
	gateway                       bool
}

type groupRow struct{ id, name string }

func (s *Server) groupRow(ctx context.Context, q interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}) (*groupRow, error) {
	g := &groupRow{}
	err := q.QueryRowContext(ctx, `SELECT id, name FROM groups ORDER BY rowid LIMIT 1`).Scan(&g.id, &g.name)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return g, err
}

// loadLog rebuilds the log from its stored entries.
func (s *Server) loadLog(ctx context.Context, tx *sql.Tx, groupID string) (*membership.Log, error) {
	rows, err := tx.QueryContext(ctx, `SELECT body FROM membership_entries WHERE group_id=? ORDER BY depth, id`, groupID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	l := membership.NewLog(groupID)
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			return nil, err
		}
		e, err := membership.ParseWire(b)
		if err != nil {
			return nil, fmt.Errorf("a stored membership entry is damaged: %w", err)
		}
		if r := l.Add(e); r.Status != membership.StatusApplied {
			return nil, fmt.Errorf("a stored membership entry (%s) is %s: %s", e.ID[:8], r.Status, r.Reason)
		}
	}
	return l, rows.Err()
}

func (s *Server) saveEntry(ctx context.Context, tx *sql.Tx, groupID string, l *membership.Log, e *membership.Entry) error {
	wire, err := e.Wire()
	if err != nil {
		return err
	}
	depth, _ := l.Depth(e.ID)
	_, err = tx.ExecContext(ctx, `INSERT OR IGNORE INTO membership_entries(id,group_id,author,type,body,sig,depth,received_at,status) VALUES(?,?,?,?,?,?,?,?,?)`,
		e.ID, groupID, e.Author, string(e.Type), wire, e.Sig, depth, s.now().Format(time.RFC3339), "applied")
	return err
}

func (s *Server) desiredMembers(ctx context.Context, tx *sql.Tx) ([]desiredMember, error) {
	var out []desiredMember
	rows, err := tx.QueryContext(ctx, `SELECT d.id, d.name, COALESCE(d.short_name,''), COALESCE(k.public_key,''), d.tunnel_ip
		FROM devices d LEFT JOIN device_keys k ON k.device_id=d.id AND k.state='active' WHERE d.status <> 'removed' ORDER BY d.added_at, d.id`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var m desiredMember
		if err := rows.Scan(&m.id, &m.name, &m.label, &m.key, &m.address); err != nil {
			rows.Close()
			return nil, err
		}
		out = append(out, m)
	}
	rows.Close()
	srows, err := tx.QueryContext(ctx, `SELECT id, name, public_key FROM servers WHERE role IN ('active','standby','retiring') AND public_key <> '' ORDER BY created_at, id`)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	for srows.Next() {
		m := desiredMember{gateway: true}
		if err := srows.Scan(&m.id, &m.name, &m.key); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, srows.Err()
}

func memberBody(m desiredMember) map[string]any {
	b := map[string]any{"id": m.id, "name": m.name}
	if m.label != "" {
		b["hostname_label"] = m.label
	}
	if m.key != "" {
		b["wg_pub"] = m.key
	}
	return b
}

// reconcileGroup brings the log in line with the devices and servers tables, inside the caller's transaction.
func (s *Server) reconcileGroup(ctx context.Context, tx *sql.Tx) error {
	n, err := s.network(ctx, tx)
	if errors.Is(err, sql.ErrNoRows) {
		return nil // nothing has been set up yet
	}
	if err != nil {
		return err
	}
	if s.d.SigningKey == nil {
		return nil
	}
	signer := membership.KeySigner{Key: s.d.SigningKey}
	authorityID := membership.MemberID(signer.Public())
	nowText := s.now().Format(time.RFC3339)

	g, err := s.groupRow(ctx, tx)
	if err != nil {
		return err
	}
	var l *membership.Log
	if g == nil {
		genesis := &membership.Entry{Author: authorityID, Parents: []string{}, Type: membership.TypeGenesis, ObservedAt: nowText,
			Body: map[string]any{"name": n.Name, "policy": string(membership.ModeAuthority), "founder": map[string]any{
				"id": authorityID, "name": authorityName, "signing_pub": base64.StdEncoding.EncodeToString(signer.Public())}}}
		if err := genesis.Sign(signer); err != nil {
			return err
		}
		l = membership.NewLog(genesis.ID)
		if r := l.Add(genesis); r.Status != membership.StatusApplied {
			return fmt.Errorf("the group could not be started: %s", r.Reason)
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO groups(id,name,mode,genesis_hash,created_at) VALUES(?,?,?,?,?)`,
			genesis.ID, n.Name, string(membership.ModeAuthority), genesis.ID, nowText); err != nil {
			return err
		}
		if err := s.saveEntry(ctx, tx, genesis.ID, l, genesis); err != nil {
			return err
		}
		g = &groupRow{id: genesis.ID, name: n.Name}
	} else if l, err = s.loadLog(ctx, tx, g.id); err != nil {
		return err
	}

	// Only the control plane's own key may write to a managed group.
	if l.State().Authority != authorityID {
		return errors.New("this group's authority is not this control plane's key, so it cannot be written to")
	}

	desired, err := s.desiredMembers(ctx, tx)
	if err != nil {
		return err
	}
	want := map[string]desiredMember{}
	for _, m := range desired {
		want[m.id] = m
	}
	write := func(typ membership.Type, body map[string]any) error {
		e := &membership.Entry{GroupID: g.id, Author: authorityID, Parents: l.Heads(), Type: typ, Body: body, ObservedAt: nowText}
		if err := e.Sign(signer); err != nil {
			return err
		}
		if r := l.Add(e); r.Status != membership.StatusApplied {
			return fmt.Errorf("the membership record refused %s: %s", typ, r.Reason)
		}
		return s.saveEntry(ctx, tx, g.id, l, e)
	}

	state := l.State()
	// New members first, in a fixed order, then key changes, then removals.
	for _, m := range desired {
		if _, known := state.Members[m.id]; known {
			continue
		}
		if err := write(membership.TypeJoin, map[string]any{"member": memberBody(m)}); err != nil {
			return err
		}
		if m.gateway {
			if err := write(membership.TypeSetNodeRoles, map[string]any{"subject": m.id, "relay": false, "gateway": true, "voter": false}); err != nil {
				return err
			}
		}
	}
	state = l.State()
	for _, m := range desired {
		cur := state.Members[m.id]
		if cur == nil || cur.Removed || m.key == "" || cur.WGPub == m.key {
			continue
		}
		if err := write(membership.TypeMemberKeyRotate, map[string]any{"subject": m.id, "wg_pub": m.key}); err != nil {
			return err
		}
	}
	state = l.State()
	var gone []string
	for _, m := range state.ActiveMembers() {
		if _, ok := want[m.ID]; !ok && m.ID != state.Authority {
			gone = append(gone, m.ID)
		}
	}
	sort.Strings(gone)
	for _, id := range gone {
		if err := write(membership.TypeRemoveMember, map[string]any{"subject": id, "cascade": false}); err != nil {
			return err
		}
	}
	return s.writeMembersTable(ctx, tx, g.id, l.State(), want)
}

// writeMembersTable rewrites the projection of the log.
func (s *Server) writeMembersTable(ctx context.Context, tx *sql.Tx, groupID string, st *membership.State, want map[string]desiredMember) error {
	if _, err := tx.ExecContext(ctx, `DELETE FROM members WHERE group_id=?`, groupID); err != nil {
		return err
	}
	ids := make([]string, 0, len(st.Members))
	for id := range st.Members {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		m := st.Members[id]
		var roles []string
		if m.Admin {
			roles = append(roles, "admin")
		}
		if m.Roles.Relay {
			roles = append(roles, "relay")
		}
		if m.Roles.Gateway {
			roles = append(roles, "gateway")
		}
		if m.Roles.Voter {
			roles = append(roles, "voter")
		}
		status := "active"
		if m.Removed {
			status = "removed"
		}
		addr := sql.NullString{}
		if d, ok := want[id]; ok && d.address != "" {
			addr = sql.NullString{String: d.address, Valid: true}
		}
		if _, err := tx.ExecContext(ctx, `INSERT INTO members(group_id,member_id,name,hostname_label,signing_pubkey,wireguard_pubkey,overlay_address,roles,status,added_by,added_entry,removed_entry)
			VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`, groupID, id, m.Name, nullIfEmpty(m.HostnameLabel), nullIfEmpty(m.SigningPub), nullIfEmpty(m.WGPub), addr,
			strings.Join(roles, ","), status, nullIfEmpty(m.AddedBy), nullIfEmpty(m.AddedEntry), nullIfEmpty(m.RemovedEntry)); err != nil {
			return err
		}
	}
	return nil
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// backfillGroup starts the log for a network that was set up before there was one.
func (s *Server) backfillGroup(ctx context.Context) error {
	tx, err := s.d.Store.DB().BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := s.reconcileGroup(ctx, tx); err != nil {
		return err
	}
	return tx.Commit()
}
