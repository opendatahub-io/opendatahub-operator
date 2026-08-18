package datasciencepipelines

import "fmt"

type UpgradeBlockedError struct {
	StoredVersion string
}

func (e *UpgradeBlockedError) Error() string {
	return fmt.Sprintf(
		"DataSciencePipelinesApplication CRD still stores deprecated version %s",
		e.StoredVersion,
	)
}
