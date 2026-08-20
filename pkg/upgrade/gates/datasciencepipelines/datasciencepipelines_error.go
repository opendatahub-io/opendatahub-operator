package datasciencepipelines

import (
	"fmt"
	"strings"
)

type UpgradeBlockedError struct {
	StoredVersion              string
	RolesMissingAPISubresource []string
}

func (e *UpgradeBlockedError) Error() string {
	parts := make([]string, 0, 2)
	if e.StoredVersion != "" {
		parts = append(parts,
			fmt.Sprintf("DataSciencePipelinesApplication CRD still stores deprecated version %s", e.StoredVersion))
	}
	if len(e.RolesMissingAPISubresource) > 0 {
		parts = append(parts,
			fmt.Sprintf("%d Role(s) still grant route access without datasciencepipelinesapplications/api", len(e.RolesMissingAPISubresource)))
	}

	return strings.Join(parts, "; ")
}
