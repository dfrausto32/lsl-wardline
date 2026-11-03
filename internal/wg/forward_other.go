//go:build !linux && !windows

package wg

import (
	"context"
	"errors"
)

func platformRelay(context.Context, string, RelayPolicy) error {
	return errors.New("relaying between devices is not supported on this system yet")
}
