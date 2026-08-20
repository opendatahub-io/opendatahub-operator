package ray

import "fmt"

type UpgradeBlockedError struct {
	CodeFlareManagedRayClusters int
}

func (e *UpgradeBlockedError) Error() string {
	return fmt.Sprintf(
		"%d CodeFlare-managed RayClusters still require pre-upgrade backup acknowledgement",
		e.CodeFlareManagedRayClusters,
	)
}
