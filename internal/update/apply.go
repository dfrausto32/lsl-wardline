package update

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// ErrRolledBack means the new version did not come up healthy, so the old one was put back.
var ErrRolledBack = errors.New("the new version did not start properly, so the old version was put back")

// Paths are the places an update touches. They are fields so tests can use a temporary folder.
type Paths struct {
	Binary     string   // /usr/local/bin/wardline
	SystemdDir string   // /etc/systemd/system
	DataDirs   []string // folders backed up before anything changes, such as /var/lib/wardline-control
	StateDir   string   // where the updater keeps the previous version and what it has learned
}

// Host is the computer's service manager.
type Host interface {
	Units() []string // wardline services installed here, such as wardline-allinone (without .service)
	DaemonReload() error
	Stop(unit string) error
	Start(unit string) error
	Healthy(ctx context.Context) error // is everything that should be running, running and answering
}

const keepBackups = 3

// updaterUnits belong to every computer that updates itself, so they follow the release too.
var updaterUnits = []string{"wardline-update.service", "wardline-update.timer"}

// Apply installs the package: stop, back up, swap the program and service files, start, check. If the
// check fails it puts the old version back and remembers the bad one so it is not tried again.
func Apply(ctx context.Context, tarball, tag string, p Paths, h Host) error {
	stage, err := os.MkdirTemp("", "wardline-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(stage)
	if err := extract(tarball, stage); err != nil {
		return err
	}
	newBin := filepath.Join(stage, "wardline")
	if st, err := os.Stat(newBin); err != nil || !st.Mode().IsRegular() {
		return errors.New("the package has no wardline program")
	}

	units := h.Units()
	// Which service files to replace: the ones this computer runs, plus the updater's own.
	var unitFiles []string
	for _, u := range units {
		unitFiles = append(unitFiles, u+".service")
	}
	unitFiles = append(unitFiles, updaterUnits...)

	prev := filepath.Join(p.StateDir, "previous")
	if err := os.RemoveAll(prev); err != nil {
		return err
	}
	if err := os.MkdirAll(prev, 0o700); err != nil {
		return err
	}
	if err := copyFile(p.Binary, filepath.Join(prev, "wardline"), 0o755); err != nil {
		return fmt.Errorf("could not keep the current program: %w", err)
	}
	had := map[string]bool{}
	for _, f := range unitFiles {
		if err := copyFile(filepath.Join(p.SystemdDir, f), filepath.Join(prev, f), 0o644); err == nil {
			had[f] = true
		}
	}

	for i := len(units) - 1; i >= 0; i-- {
		if err := h.Stop(units[i]); err != nil {
			return fmt.Errorf("could not stop %s: %w", units[i], err)
		}
	}
	// With nothing running, the data is safe to copy.
	for _, d := range p.DataDirs {
		if _, err := os.Stat(d); err == nil {
			if err := copyTree(d, d+".backup-before-"+tag); err != nil {
				startAll(h, units)
				return fmt.Errorf("could not back up %s, so nothing was changed: %w", d, err)
			}
		}
	}
	pruneBackups(p.DataDirs)

	rollback := func(cause error) error {
		replaceFile(filepath.Join(prev, "wardline"), p.Binary, 0o755)
		for _, f := range unitFiles {
			if had[f] {
				replaceFile(filepath.Join(prev, f), filepath.Join(p.SystemdDir, f), 0o644)
			} else {
				os.Remove(filepath.Join(p.SystemdDir, f))
			}
		}
		h.DaemonReload()
		for _, u := range units {
			h.Stop(u)
		}
		startAll(h, units)
		markBad(p, tag, cause)
		h.Healthy(ctx)
		return fmt.Errorf("%w (%v)", ErrRolledBack, cause)
	}

	if err := replaceFile(newBin, p.Binary, 0o755); err != nil {
		return rollback(err)
	}
	for _, f := range unitFiles {
		src := filepath.Join(stage, f)
		if _, err := os.Stat(src); err != nil {
			continue
		}
		if err := replaceFile(src, filepath.Join(p.SystemdDir, f), 0o644); err != nil {
			return rollback(err)
		}
	}
	if err := h.DaemonReload(); err != nil {
		return rollback(err)
	}
	if err := startAll(h, units); err != nil {
		return rollback(err)
	}
	if err := h.Healthy(ctx); err != nil {
		return rollback(err)
	}
	return nil
}

func startAll(h Host, units []string) error {
	var first error
	for _, u := range units {
		if err := h.Start(u); err != nil && first == nil {
			first = fmt.Errorf("could not start %s: %w", u, err)
		}
	}
	return first
}

// ---- remembering a version that failed ----

func badFile(p Paths, tag string) string {
	return filepath.Join(p.StateDir, "bad-"+strings.ReplaceAll(tag, "/", "_"))
}

func markBad(p Paths, tag string, cause error) {
	os.WriteFile(badFile(p, tag), []byte(cause.Error()+"\n"), 0o600)
}

// IsKnownBad says whether this version already failed here. Remove the file in the state folder to try it again.
func IsKnownBad(p Paths, tag string) bool {
	_, err := os.Stat(badFile(p, tag))
	return err == nil
}

// ---- files ----

// extract unpacks only plain files and folders, and refuses anything that would land outside dir.
func extract(tarball, dir string) error {
	f, err := os.Open(tarball)
	if err != nil {
		return err
	}
	defer f.Close()
	gz, err := gzip.NewReader(f)
	if err != nil {
		return err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
		name := path.Clean(strings.TrimPrefix(h.Name, "./"))
		if name == "." {
			continue
		}
		if path.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			return fmt.Errorf("the package has an unsafe path: %s", h.Name)
		}
		dest := filepath.Join(dir, filepath.FromSlash(name))
		switch h.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(dest, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(dest), 0o755); err != nil {
				return err
			}
			out, err := os.OpenFile(dest, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
			if err != nil {
				return err
			}
			_, err = io.Copy(out, io.LimitReader(tr, 200<<20))
			if cerr := out.Close(); err == nil {
				err = cerr
			}
			if err != nil {
				return err
			}
		default: // links, devices and the like are never needed
		}
	}
}

func copyFile(src, dst string, mode fs.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	return os.WriteFile(dst, b, mode)
}

// replaceFile puts src in place at dst in one step (write beside it, then rename), so a program that is
// running or about to be started never sees half a file.
func replaceFile(src, dst string, mode fs.FileMode) error {
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".new"
	if err := os.WriteFile(tmp, b, mode); err != nil {
		return err
	}
	if err := os.Chmod(tmp, mode); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, dst); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

func copyTree(src, dst string) error {
	if err := os.RemoveAll(dst); err != nil {
		return err
	}
	return filepath.WalkDir(src, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		target := filepath.Join(dst, rel)
		info, err := d.Info()
		if err != nil {
			return err
		}
		if d.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm()|0o700)
		}
		if !info.Mode().IsRegular() {
			return nil
		}
		return copyFile(p, target, info.Mode().Perm())
	})
}

// pruneBackups keeps the newest few backups of each data folder.
func pruneBackups(dirs []string) {
	for _, d := range dirs {
		matches, _ := filepath.Glob(d + ".backup-before-*")
		sort.Slice(matches, func(i, j int) bool {
			a, okA := parseVersion(strings.TrimPrefix(matches[i], d+".backup-before-"))
			b, okB := parseVersion(strings.TrimPrefix(matches[j], d+".backup-before-"))
			if okA != okB {
				return okA
			}
			for k := range a {
				if a[k] != b[k] {
					return a[k] > b[k]
				}
			}
			return matches[i] > matches[j]
		})
		for i := keepBackups; i < len(matches); i++ {
			os.RemoveAll(matches[i])
		}
	}
}
