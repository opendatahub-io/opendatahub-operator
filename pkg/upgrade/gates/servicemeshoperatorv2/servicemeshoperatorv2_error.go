package servicemeshoperatorv2

import "fmt"

type UpgradeBlockedError struct {
	SubscriptionNamespace string
	SubscriptionName      string
	Channel               string
	InstalledCSV          string
}

func (e *UpgradeBlockedError) Error() string {
	if e.InstalledCSV != "" {
		return fmt.Sprintf(
			"Service Mesh Operator v2 subscription %s/%s is still installed on channel %s (CSV %s) and should be removed",
			e.SubscriptionNamespace,
			e.SubscriptionName,
			e.Channel,
			e.InstalledCSV,
		)
	}

	return fmt.Sprintf(
		"Service Mesh Operator v2 subscription %s/%s is still installed on channel %s and should be removed",
		e.SubscriptionNamespace,
		e.SubscriptionName,
		e.Channel,
	)
}
