package workbenches

import (
	"fmt"
	"strings"
)

type UpgradeBlockedError struct {
	NotebooksWithBrokenHardwareProfileRefs int
	NotebooksWithBrokenConnectionRefs      int
	NotebooksWithContainerNameMismatch     int
}

func (e *UpgradeBlockedError) Error() string {
	parts := make([]string, 0, 2)
	if e.NotebooksWithBrokenHardwareProfileRefs > 0 {
		parts = append(parts, fmt.Sprintf(
			"%d Notebooks reference missing HardwareProfiles",
			e.NotebooksWithBrokenHardwareProfileRefs,
		))
	}
	if e.NotebooksWithBrokenConnectionRefs > 0 {
		parts = append(parts, fmt.Sprintf(
			"%d Notebooks reference missing connection Secrets",
			e.NotebooksWithBrokenConnectionRefs,
		))
	}
	if e.NotebooksWithContainerNameMismatch > 0 {
		parts = append(parts, fmt.Sprintf(
			"%d Dashboard-managed Notebooks have a container name mismatch",
			e.NotebooksWithContainerNameMismatch,
		))
	}

	return "workbenches blocking workloads found: " + strings.Join(parts, ", ")
}
