package store

import (
	"path/filepath"
	"strings"
	"testing"
)

func open(t *testing.T) *Store {
	t.Helper()
	s, err := Open(filepath.Join(t.TempDir(), "w.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestOpenCreatesAllTables(t *testing.T) {
	s := open(t)
	want := []string{"networks", "servers", "people", "devices", "device_keys", "preshared_keys",
		"enrollment_tokens", "ip_allocations", "admin_users", "sessions", "audit_events", "server_moves", "settings"}
	for _, tbl := range want {
		var n int
		err := s.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name=?`, tbl).Scan(&n)
		if err != nil || n != 1 {
			t.Errorf("table %s missing (err=%v)", tbl, err)
		}
	}
}

func TestMigrationsAreIdempotent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "w.db")
	for i := 0; i < 3; i++ {
		s, err := Open(path)
		if err != nil {
			t.Fatalf("open #%d: %v", i, err)
		}
		s.Close()
	}
}

func TestNoPrivateKeyColumnAnywhere(t *testing.T) {
	s := open(t)
	rows, err := s.DB().Query(`SELECT m.name, p.name FROM sqlite_master m, pragma_table_info(m.name) p WHERE m.type='table'`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var tbl, col string
		rows.Scan(&tbl, &col)
		l := strings.ToLower(col)
		if strings.Contains(l, "private") {
			t.Errorf("column %s.%s looks like private key storage", tbl, col)
		}
	}
}

func TestForeignKeysAreEnforced(t *testing.T) {
	s := open(t)
	_, err := s.DB().Exec(`INSERT INTO people(id, network_id, display_name, created_at) VALUES('p1','nope','Sam','2026-01-01T00:00:00Z')`)
	if err == nil {
		t.Fatal("expected foreign key violation")
	}
}

func seedNetwork(t *testing.T, s *Store) {
	t.Helper()
	mustExec(t, s, `INSERT INTO networks(id,name,address_range,server_address,server_address_kind,created_at) VALUES('n1','Home','10.77.0.0/24','vpn.example.com','name','2026-01-01T00:00:00Z')`)
	mustExec(t, s, `INSERT INTO people(id,network_id,display_name,created_at) VALUES('p1','n1','Sam','2026-01-01T00:00:00Z')`)
}

func mustExec(t *testing.T, s *Store, q string, args ...any) {
	t.Helper()
	if _, err := s.DB().Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

func addDevice(s *Store, id, name, ip, status string) error {
	_, err := s.DB().Exec(`INSERT INTO devices(id,network_id,person_id,name,platform,kind,tunnel_ip,status,added_at) VALUES(?,?,?,?,?,?,?,?,?)`,
		id, "n1", "p1", name, "ios", "manual", ip, status, "2026-01-01T00:00:00Z")
	return err
}

func TestDeviceNameUniquePerNetwork(t *testing.T) {
	s := open(t)
	seedNetwork(t, s)
	if err := addDevice(s, "d1", "Sam's iPhone", "10.77.0.2", "waiting"); err != nil {
		t.Fatal(err)
	}
	if err := addDevice(s, "d2", "Sam's iPhone", "10.77.0.3", "waiting"); err == nil {
		t.Fatal("duplicate device name should be rejected")
	}
}

func TestTunnelIPUniqueAmongNonRemovedDevices(t *testing.T) {
	s := open(t)
	seedNetwork(t, s)
	if err := addDevice(s, "d1", "A", "10.77.0.2", "waiting"); err != nil {
		t.Fatal(err)
	}
	if err := addDevice(s, "d2", "B", "10.77.0.2", "waiting"); err == nil {
		t.Fatal("duplicate tunnel ip should be rejected")
	}
	mustExec(t, s, `UPDATE devices SET status='removed', removed_at='2026-02-01T00:00:00Z' WHERE id='d1'`)
	if err := addDevice(s, "d3", "C", "10.77.0.2", "waiting"); err != nil {
		t.Fatalf("a removed device must not block reuse at the schema level (quarantine is ipam's job): %v", err)
	}
}

func TestActiveKeyUniqueAcrossDevices(t *testing.T) {
	s := open(t)
	seedNetwork(t, s)
	addDevice(s, "d1", "A", "10.77.0.2", "waiting")
	addDevice(s, "d2", "B", "10.77.0.3", "waiting")
	ins := `INSERT INTO device_keys(id,device_id,public_key,state,created_at) VALUES(?,?,?,?,?)`
	mustExec(t, s, ins, "k1", "d1", "PUBKEY1", "active", "2026-01-01T00:00:00Z")
	if _, err := s.DB().Exec(ins, "k2", "d2", "PUBKEY1", "active", "2026-01-01T00:00:00Z"); err == nil {
		t.Fatal("same public key active on two devices should be rejected")
	}
	if _, err := s.DB().Exec(ins, "k3", "d1", "PUBKEY2", "active", "2026-01-01T00:00:00Z"); err == nil {
		t.Fatal("two active keys on one device should be rejected")
	}
	mustExec(t, s, ins, "k4", "d1", "PUBKEY3", "pending", "2026-01-01T00:00:00Z")
	if _, err := s.DB().Exec(ins, "k5", "d1", "PUBKEY4", "pending", "2026-01-01T00:00:00Z"); err == nil {
		t.Fatal("two pending keys on one device should be rejected")
	}
}

func TestCheckConstraintsOnEnums(t *testing.T) {
	s := open(t)
	seedNetwork(t, s)
	if err := addDevice(s, "d1", "A", "10.77.0.2", "bogus"); err == nil {
		t.Fatal("bad status should be rejected")
	}
}

func TestNewIDIsULIDShapedAndSortable(t *testing.T) {
	a := NewID()
	b := NewID()
	if len(a) != 26 || len(b) != 26 {
		t.Fatalf("ulid length: %q %q", a, b)
	}
	if a == b {
		t.Fatal("ids must be unique")
	}
}

func TestNowIsUTCRFC3339(t *testing.T) {
	if !strings.HasSuffix(Now(), "Z") {
		t.Fatal(Now())
	}
}
