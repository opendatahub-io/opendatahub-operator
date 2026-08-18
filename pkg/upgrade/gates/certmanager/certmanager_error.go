package certmanager

import "fmt"

type UpgradeBlockedError struct {
	SubscriptionNamespace string
	SubscriptionName      string
}

func (e *UpgradeBlockedError) Error() string {
	return fmt.Sprintf(
		"cert-manager subscription %s/%s not found",
		e.SubscriptionNamespace,
		e.SubscriptionName,
	)
}
