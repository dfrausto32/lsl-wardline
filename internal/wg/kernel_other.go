//go:build !linux

package wg

import (
	"context"
	"errors"
)

var errNoKernel = errors.New("the kernel WireGuard backend is only available on Linux")

// Kernel is a placeholder on systems without the Linux kernel backend.
type Kernel struct{}

func NewKernel() (*Kernel, error)                                        { return nil, errNoKernel }
func (*Kernel) Close() error                                             { return nil }
func (*Kernel) Configure(context.Context, string, InterfaceConfig) error { return errNoKernel }
func (*Kernel) Peers(context.Context, string) ([]PeerState, error)       { return nil, errNoKernel }
func (*Kernel) ApplyDiff(context.Context, string, Diff) error            { return errNoKernel }
func (*Kernel) Remove(context.Context, string) error                     { return errNoKernel }

// KernelAvailable is always false off Linux.
func KernelAvailable() bool { return false }
