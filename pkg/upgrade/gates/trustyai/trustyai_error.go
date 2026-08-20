package trustyai

import "fmt"

type UpgradeBlockedError struct {
	PVCStorageTrustyAIServices int
}

func (e *UpgradeBlockedError) Error() string {
	return fmt.Sprintf(
		"%d TrustyAIService instances using PVC storage require pre-upgrade backup",
		e.PVCStorageTrustyAIServices,
	)
}
