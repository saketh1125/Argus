package services

import (
	"context"
	"errors"
)

// ErrSandboxUnavailable indicates that no verified isolated executor is configured.
var ErrSandboxUnavailable = errors.New("isolated sandbox unavailable")

type unavailableSandbox struct{}

func (unavailableSandbox) Run(context.Context, SandboxSpec) (*SandboxResult, error) {
	return nil, ErrSandboxUnavailable
}
func (unavailableSandbox) Name() string { return "unavailable" }
func (unavailableSandbox) Live() bool   { return false }
