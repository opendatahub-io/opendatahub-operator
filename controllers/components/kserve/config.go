package kserve

import (
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
)

// ConfigMap Keys.
const (
	DeployConfigName     = "deploy"
	IngressConfigKeyName = "ingress"
	ServiceConfigKeyName = "service"
)

type DeployConfig struct {
	DefaultDeploymentMode string `json:"defaultDeploymentMode,omitempty"`
}

func getDeployConfig(cm *corev1.ConfigMap) (*DeployConfig, error) {
	deployConfig := DeployConfig{}
	if err := json.Unmarshal([]byte(cm.Data[DeployConfigName]), &deployConfig); err != nil {
		return nil, fmt.Errorf("error retrieving value for key '%s' from ConfigMap %s. %w", DeployConfigName, cm.Name, err)
	}
	return &deployConfig, nil
}
