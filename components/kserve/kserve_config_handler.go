package kserve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/go-logr/logr"
	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
)

const (
	KserveConfigMapName string = "inferenceservice-config"
)

func (k *Kserve) setupKserveConfig(ctx context.Context, cli client.Client, logger logr.Logger, dscispec *dsciv1.DSCInitializationSpec) error {
	// as long as Kserve.Serving is not 'Removed', we will setup the dependencies

	defaultDeploymentMode := k.DefaultDeploymentMode
	if defaultDeploymentMode == "" {
		defaultDeploymentMode = Serverless
	}
	disableIngressCreation := true
	if k.RawRouteCreation == operatorv1.Managed {
		disableIngressCreation = false
	}
	switch k.Serving.ManagementState {
	case operatorv1.Managed, operatorv1.Unmanaged:
		if err := k.setKserveRawConfig(ctx, cli, dscispec, defaultDeploymentMode, disableIngressCreation); err != nil {
			return err
		}
	case operatorv1.Removed:
		if k.DefaultDeploymentMode == Serverless {
			return errors.New("setting defaultdeployment mode as Serverless is incompatible with having Serving 'Removed'")
		}
		if k.DefaultDeploymentMode == "" {
			logger.Info("Serving is removed, Kserve will default to rawdeployment")
		}
		if err := k.setKserveRawConfig(ctx, cli, dscispec, RawDeployment, disableIngressCreation); err != nil {
			return err
		}
	}
	return nil
}

func (k *Kserve) setKserveRawConfig(
	ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec,
	defaultmode DefaultDeploymentMode, disableIngressCreation bool) error {
	inferenceServiceConfigMap := &corev1.ConfigMap{}
	err := cli.Get(ctx, client.ObjectKey{
		Namespace: dscispec.ApplicationsNamespace,
		Name:      KserveConfigMapName,
	}, inferenceServiceConfigMap)
	if err != nil {
		return fmt.Errorf("error getting configmap %v: %w", KserveConfigMapName, err)
	}

	// set data.deploy.defaultDeploymentMode to the model specified in the Kserve spec
	var deployData map[string]interface{}
	if err = json.Unmarshal([]byte(inferenceServiceConfigMap.Data["deploy"]), &deployData); err != nil {
		return fmt.Errorf("error retrieving value for key 'deploy' from configmap %s. %w", KserveConfigMapName, err)
	}
	var ingressData map[string]interface{}
	if err = json.Unmarshal([]byte(inferenceServiceConfigMap.Data["ingress"]), &ingressData); err != nil {
		return fmt.Errorf("error retrieving value for key 'ingress' from configmap %s. %w", KserveConfigMapName, err)
	}
	modeFound := deployData["defaultDeploymentMode"]
	ingressCreationValueFound := ingressData["disableIngressCreation"]
	if (modeFound != string(defaultmode)) || ingressCreationValueFound != disableIngressCreation {
		deployData["defaultDeploymentMode"] = defaultmode
		deployDataBytes, err := json.MarshalIndent(deployData, "", " ")
		if err != nil {
			return fmt.Errorf("could not set values in configmap %s. %w", KserveConfigMapName, err)
		}
		inferenceServiceConfigMap.Data["deploy"] = string(deployDataBytes)
		clusterDomain, err := cluster.GetDomain(ctx, cli)
		if err != nil {
			return fmt.Errorf("error retrieving cluster domain %s. %w", KserveConfigMapName, err)
		}
		ingressData["ingressDomain"] = clusterDomain
		ingressData["disableIngressCreation"] = disableIngressCreation
		ingressDataBytes, err := json.MarshalIndent(ingressData, "", " ")
		if err != nil {
			return fmt.Errorf("could not set values in configmap %s. %w", KserveConfigMapName, err)
		}
		inferenceServiceConfigMap.Data["ingress"] = string(ingressDataBytes)

		if err = cli.Update(ctx, inferenceServiceConfigMap); err != nil {
			return fmt.Errorf("could not set default deployment mode for Kserve. %w", err)
		}

		// Restart the pod if configmap is updated so that kserve boots with the correct value
		podList := &corev1.PodList{}
		listOpts := []client.ListOption{
			client.InNamespace(dscispec.ApplicationsNamespace),
			client.MatchingLabels{
				labels.ODH.Component(ComponentName): "true",
				"control-plane":                     "kserve-controller-manager",
			},
		}
		if err := cli.List(ctx, podList, listOpts...); err != nil {
			return fmt.Errorf("failed to list pods: %w", err)
		}
		for _, pod := range podList.Items {
			pod := pod
			if err := cli.Delete(ctx, &pod); err != nil {
				return fmt.Errorf("failed to delete pod %s: %w", pod.Name, err)
			}
		}
	}

	return nil
}

func (k *Kserve) configureServerless(ctx context.Context, cli client.Client, logger logr.Logger, instance *dsciv1.DSCInitializationSpec) error {
	switch k.Serving.ManagementState {
	case operatorv1.Unmanaged: // Bring your own CR
		logger.Info("Serverless CR is not configured by the operator, we won't do anything")

	case operatorv1.Removed: // we remove serving CR
		logger.Info("existing Serverless CR (owned by operator) will be removed")
		if err := k.removeServerlessFeatures(ctx, instance); err != nil {
			return err
		}

	case operatorv1.Managed: // standard workflow to create CR
		if instance.ServiceMesh == nil {
			return errors.New("ServiceMesh needs to be configured and 'Managed' in DSCI CR, " +
				"it is required by KServe serving")
		}

		switch instance.ServiceMesh.ManagementState {
		case operatorv1.Unmanaged, operatorv1.Removed:
			return fmt.Errorf("ServiceMesh is currently set to '%s'. It needs to be set to 'Managed' in DSCI CR, "+
				"as it is required by the KServe serving field", instance.ServiceMesh.ManagementState)
		}

		// check on dependent operators if all installed in cluster
		dependOpsErrors := checkDependentOperators(ctx, cli).ErrorOrNil()
		if dependOpsErrors != nil {
			return dependOpsErrors
		}

		serverlessFeatures := feature.ComponentFeaturesHandler(k.GetComponentName(), instance.ApplicationsNamespace, k.configureServerlessFeatures(instance))

		if err := serverlessFeatures.Apply(ctx); err != nil {
			return err
		}
	}
	return nil
}

func (k *Kserve) removeServerlessFeatures(ctx context.Context, instance *dsciv1.DSCInitializationSpec) error {
	serverlessFeatures := feature.ComponentFeaturesHandler(k.GetComponentName(), instance.ApplicationsNamespace, k.configureServerlessFeatures(instance))

	return serverlessFeatures.Delete(ctx)
}

func checkDependentOperators(ctx context.Context, cli client.Client) *multierror.Error {
	var multiErr *multierror.Error

	if found, err := cluster.OperatorExists(ctx, cli, ServiceMeshOperator); err != nil {
		multiErr = multierror.Append(multiErr, err)
	} else if !found {
		err = fmt.Errorf("operator %s not found. Please install the operator before enabling %s component",
			ServiceMeshOperator, ComponentName)
		multiErr = multierror.Append(multiErr, err)
	}

	if found, err := cluster.OperatorExists(ctx, cli, ServerlessOperator); err != nil {
		multiErr = multierror.Append(multiErr, err)
	} else if !found {
		err = fmt.Errorf("operator %s not found. Please install the operator before enabling %s component",
			ServerlessOperator, ComponentName)
		multiErr = multierror.Append(multiErr, err)
	}
	return multiErr
}
