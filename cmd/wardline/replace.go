package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// replaceFile makes the file at `to` whatever write produces. Windows will not let a program that is
// running be overwritten, but it does let it be renamed, so when the write fails the old file is moved
// aside and the write is tried again. The moved-aside file is deleted by removeAside next time.
func replaceFile(to string, write func(path string) error) error {
	err := write(to)
	if err == nil {
		return nil
	}
	if _, serr := os.Stat(to); serr != nil {
		return err // the file is not there to be in use; this is some other problem
	}
	aside := to + ".old"
	if rerr := os.Remove(aside); rerr != nil && !os.IsNotExist(rerr) {
		aside = fmt.Sprintf("%s.old%d", to, time.Now().UnixNano()) // the last one is itself still in use
	}
	if rerr := os.Rename(to, aside); rerr != nil {
		return fmt.Errorf("the old copy of Wardline is still running and cannot be replaced: %w", err)
	}
	if err := write(to); err != nil {
		os.Rename(aside, to) // put it back rather than leave nothing
		return err
	}
	return nil
}

// removeAside deletes files that replaceFile moved aside, as far as they are no longer in use.
func removeAside(to string) {
	os.Remove(to + ".old")
	if matches, err := globOld(to); err == nil {
		for _, m := range matches {
			os.Remove(m)
		}
	}
}

func globOld(to string) ([]string, error) { return filepath.Glob(to + ".old*") }
