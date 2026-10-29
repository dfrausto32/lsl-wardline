//go:build windows

package wintun

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed amd64/wintun.dll
var dll []byte

// Prepare puts the driver library next to this program, where the tunnel code loads it from.
func Prepare() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return PrepareIn(filepath.Dir(exe))
}

// PrepareIn puts the driver library in dir, for a copy of the program that is installed there.
func PrepareIn(dir string) error {
	if err := Ensure(dir, dll); err != nil {
		return fmt.Errorf("could not place the network driver next to the program (run it as administrator): %w", err)
	}
	return nil
}
