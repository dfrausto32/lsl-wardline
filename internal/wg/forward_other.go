//go:build !linux && !windows

package wg

import (
	"context"
	"errors"
)

func platformForwarding(context.Context, string, bool) error {
	return errors.New("relaying between devices is not supported on this system yet")
}
