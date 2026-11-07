package services

import (
	"context"
	"fmt"
)

// unavailableSandbox refuses validation until a provider has a verified,
// bounded API implementation. It never materializes files or runs repository
// supplied code on the host.
type unavailableSandbox struct {
	reason string
}

func newUnavailableSandbox(reason string) *unavailableSandbox {
	return &unavailableSandbox{reason: reason}
}

func (u *unavailableSandbox) Run(context.Context, SandboxSpec) (*SandboxResult, error) {
	return nil, fmt.Errorf("validation unavailable: %s", u.reason)
}

func (u *unavailableSandbox) Name() string { return "unavailable" }

func (u *unavailableSandbox) Live() bool { return false }
