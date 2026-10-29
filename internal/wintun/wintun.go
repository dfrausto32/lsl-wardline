// Package wintun carries the official, unmodified Wintun driver library
// (wintun.dll, from wintun.net/builds, version 0.14.1) so the Windows program
// is one file. The licence in LICENSE.txt allows shipping it alongside a
// program that uses it only through the public interface (wintun.h), which is
// all wireguard-go does. The file must never be changed; a test pins its hash.
package wintun

import (
	"bytes"
	"os"
	"path/filepath"
)

// DLLName is the file the tunnel library looks for next to the program.
const DLLName = "wintun.dll"

// Ensure makes dir/wintun.dll exactly data. A file that already matches is left alone.
func Ensure(dir string, data []byte) error {
	path := filepath.Join(dir, DLLName)
	if have, err := os.ReadFile(path); err == nil && bytes.Equal(have, data) {
		return nil
	}
	tmp, err := os.CreateTemp(dir, DLLName+".*")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(data)
	cerr := tmp.Close()
	if werr != nil || cerr != nil {
		os.Remove(tmp.Name())
		if werr != nil {
			return werr
		}
		return cerr
	}
	if err := os.Chmod(tmp.Name(), 0o755); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}
