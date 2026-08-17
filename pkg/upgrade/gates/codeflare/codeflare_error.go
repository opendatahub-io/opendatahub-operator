package codeflare

import (
	"fmt"
	"strings"
)

type UpgradeBlockedError struct {
	CodeFlareCRPresent bool
	AppWrappers        int
}

func (e *UpgradeBlockedError) Error() string {
	parts := make([]string, 0, 2)
	if e.CodeFlareCRPresent {
		parts = append(parts, "CodeFlare internal CR present")
	}
	if e.AppWrappers > 0 {
		parts = append(parts, fmt.Sprintf("%d AppWrappers present", e.AppWrappers))
	}

	return strings.Join(parts, ", ")
}
