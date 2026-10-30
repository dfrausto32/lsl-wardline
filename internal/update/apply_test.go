package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func makeTarball(t *testing.T, files map[string]string) string {
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	for name, body := range files {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o644, Size: int64(len(body)), Typeflag: tar.TypeReg}); err != nil {
			t.Fatal(err)
		}
		tw.Write([]byte(body))
	}
	tw.Close()
	gz.Close()
	p := filepath.Join(t.TempDir(), "pkg.tar.gz")
	os.WriteFile(p, buf.Bytes(), 0o600)
	return p
}

type fakeHost struct {
	units     []string
	log       []string
	healthErr []error // one per call to Healthy; nil entries mean healthy
	calls     int
}

func (h *fakeHost) Units() []string      { return h.units }
func (h *fakeHost) DaemonReload() error  { h.log = append(h.log, "reload"); return nil }
func (h *fakeHost) Stop(u string) error  { h.log = append(h.log, "stop "+u); return nil }
func (h *fakeHost) Start(u string) error { h.log = append(h.log, "start "+u); return nil }
func (h *fakeHost) Healthy(context.Context) error {
	i := h.calls
	h.calls++
	if i < len(h.healthErr) {
		return h.healthErr[i]
	}
	return nil
}

type env struct {
	p    Paths
	host *fakeHost
}

func newEnv(t *testing.T) env {
	root := t.TempDir()
	p := Paths{
		Binary:     filepath.Join(root, "bin", "wardline"),
		SystemdDir: filepath.Join(root, "systemd"),
		DataDirs:   []string{filepath.Join(root, "data")},
		StateDir:   filepath.Join(root, "state"),
	}
	for _, d := range []string{filepath.Dir(p.Binary), p.SystemdDir, p.DataDirs[0]} {
		os.MkdirAll(d, 0o755)
	}
	os.WriteFile(p.Binary, []byte("old binary"), 0o755)
	os.WriteFile(filepath.Join(p.SystemdDir, "wardline-allinone.service"), []byte("old unit"), 0o644)
	os.WriteFile(filepath.Join(p.DataDirs[0], "wardline.db"), []byte("the database"), 0o600)
	return env{p: p, host: &fakeHost{units: []string{"wardline-allinone"}}}
}

func read(t *testing.T, path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

func goodPackage(t *testing.T) string {
	return makeTarball(t, map[string]string{
		"./wardline":                  "new binary",
		"./wardline-allinone.service": "new unit",
		"./wardline-agent.service":    "agent unit (not installed here)",
		"./wardline-update.service":   "update service",
		"./wardline-update.timer":     "update timer",
		"./install-linux.sh":          "#!/bin/sh",
	})
}

func TestAGoodUpdateSwapsTheProgramBacksUpAndRestarts(t *testing.T) {
	e := newEnv(t)
	if err := Apply(context.Background(), goodPackage(t), "v0.2.0", e.p, e.host); err != nil {
		t.Fatal(err)
	}
	if read(t, e.p.Binary) != "new binary" {
		t.Fatal("the program was not replaced")
	}
	if read(t, filepath.Join(e.p.SystemdDir, "wardline-allinone.service")) != "new unit" {
		t.Fatal("the installed service file was not updated")
	}
	if _, err := os.Stat(filepath.Join(e.p.SystemdDir, "wardline-agent.service")); err == nil {
		t.Fatal("a service this computer does not run was installed")
	}
	if read(t, filepath.Join(e.p.SystemdDir, "wardline-update.timer")) != "update timer" {
		t.Fatal("the updater's own timer was not kept up to date")
	}
	if got := read(t, filepath.Join(e.p.DataDirs[0]+".backup-before-v0.2.0", "wardline.db")); got != "the database" {
		t.Fatalf("backup wrong: %q", got)
	}
	if strings.Join(e.host.log, ",") != "stop wardline-allinone,reload,start wardline-allinone" {
		t.Fatalf("steps: %v", e.host.log)
	}
}

func TestAnUnhealthyUpdateIsRolledBack(t *testing.T) {
	e := newEnv(t)
	e.host.healthErr = []error{errors.New("web app did not answer")} // the new version fails; the old one is fine again
	err := Apply(context.Background(), goodPackage(t), "v0.2.0", e.p, e.host)
	if !errors.Is(err, ErrRolledBack) {
		t.Fatalf("got %v", err)
	}
	if read(t, e.p.Binary) != "old binary" {
		t.Fatal("the old program was not put back")
	}
	if read(t, filepath.Join(e.p.SystemdDir, "wardline-allinone.service")) != "old unit" {
		t.Fatal("the old service file was not put back")
	}
	if read(t, filepath.Join(e.p.DataDirs[0], "wardline.db")) != "the database" {
		t.Fatal("the data changed")
	}
	if got := e.host.log[len(e.host.log)-1]; got != "start wardline-allinone" {
		t.Fatalf("the old version was not started again: %v", e.host.log)
	}
	if !IsKnownBad(e.p, "v0.2.0") {
		t.Fatal("the failed version was not remembered, so it would be tried again every half hour")
	}
}

func TestAPackageWithoutAProgramChangesNothing(t *testing.T) {
	e := newEnv(t)
	bad := makeTarball(t, map[string]string{"./wardline-allinone.service": "new unit"})
	if err := Apply(context.Background(), bad, "v0.2.0", e.p, e.host); err == nil {
		t.Fatal("expected an error")
	}
	if read(t, e.p.Binary) != "old binary" || len(e.host.log) != 0 {
		t.Fatalf("something changed: %v", e.host.log)
	}
}

func TestPackagePathsCannotEscape(t *testing.T) {
	e := newEnv(t)
	evil := makeTarball(t, map[string]string{"./wardline": "x", "../../evil": "x"})
	if err := Apply(context.Background(), evil, "v0.2.0", e.p, e.host); err == nil {
		t.Fatal("a path that climbs out of the package must be refused")
	}
	if read(t, e.p.Binary) != "old binary" {
		t.Fatal("the program changed")
	}
}

func TestOldBackupsArePruned(t *testing.T) {
	e := newEnv(t)
	for _, v := range []string{"v0.1.1", "v0.1.2", "v0.1.3", "v0.1.4"} {
		os.MkdirAll(e.p.DataDirs[0]+".backup-before-"+v, 0o755)
	}
	if err := Apply(context.Background(), goodPackage(t), "v0.2.0", e.p, e.host); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(e.p.DataDirs[0] + ".backup-before-*")
	if len(matches) != 3 {
		t.Fatalf("kept %d backups: %v", len(matches), matches)
	}
}
