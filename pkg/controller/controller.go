package controller

import (
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/kubeflow/kfctl/v3/pkg/controller/kfdef"
	"github.com/kubeflow/kfctl/v3/pkg/controller/secretgenerator"
)

// AddToManager adds all Controllers to the Manager
func AddToManager(m manager.Manager) error {
	// Add KfDef controller
	err := kfdef.AddToManager(m)
	if err != nil {
		return err
	}

	// Add Secrets Generator controller
	err = secretgenerator.AddToManager(m)
	if err != nil {
		return err
	}

	return nil
}
