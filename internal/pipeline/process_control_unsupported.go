//go:build !windows && !linux

package pipeline

import (
	"context"
	"fmt"
	"runtime"
)

type defaultProcessController struct{}

func (defaultProcessController) StartOwned(ctx context.Context, spec CommandSpec) (OwnedProcess, error) {
	return nil, fmt.Errorf("%w: process ownership is unsupported on %s", ErrProcessUnverifiable, runtime.GOOS)
}

func (defaultProcessController) OpenOwned(identity ProcessIdentity) (OwnedProcess, error) {
	return nil, fmt.Errorf("%w: process ownership is unsupported on %s", ErrProcessUnverifiable, runtime.GOOS)
}
