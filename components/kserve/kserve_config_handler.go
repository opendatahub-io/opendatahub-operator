package kserve

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/hashicorp/go-multierror"
	operatorv1 "github.com/openshift/api/operator/v1"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/feature"
)

const (
	KserveConfigMapName string = "inferenceservice-config"
)

func (k *Kserve) setupKserveConfigAndDependencies(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec) error {
	// as long as Kserve.Serving is not 'Removed', we will setup the dependencies

	switch k.Serving.ManagementState {
	case operatorv1.Managed:
		// check on dependent operators if all installed in cluster
		dependOpsErrors := checkDependentOperators(cli).ErrorOrNil()
		if dependOpsErrors != nil {
			return dependOpsErrors
		}

		if err := k.configureServerless(dscispec); err != nil {
			return err
		}

		if err := k.setDefaultDeploymentMode(ctx, cli, dscispec, k.DefaultDeploymentMode); err != nil {
			return err
		}
	case operatorv1.Unmanaged:
		fmt.Println("Serverless is Unmanaged, Kserve will default to RawDeployment")
		if err := k.setDefaultDeploymentMode(ctx, cli, dscispec, RawDeployment); err != nil {
			return err
		}
	case operatorv1.Removed:
		if k.DefaultDeploymentMode == Serverless {
			return fmt.Errorf("setting defaultdeployment mode as Serverless is incompatible with having Serving 'Removed'")
		}
		if err := k.setDefaultDeploymentMode(ctx, cli, dscispec, RawDeployment); err != nil {
			return err
		}
	}
	return nil
}

func (k *Kserve) setDefaultDeploymentMode(ctx context.Context, cli client.Client, dscispec *dsciv1.DSCInitializationSpec, defaultmode DefaultDeploymentMode) error {
	inferenceServiceConfigMap := &corev1.ConfigMap{}
	err := cli.Get(ctx, client.ObjectKey{
		Namespace: dscispec.ApplicationsNamespace,
		Name:      KserveConfigMapName,
	}, inferenceServiceConfigMap)
	if err != nil {
		return fmt.Errorf("error getting configmap 'inferenceservice-config'. %w", err)
	}

	// set data.deploy.defaultDeploymentMode to the model specified in the Kserve spec
	var deployData map[string]interface{}
	if err = json.Unmarshal([]byte(inferenceServiceConfigMap.Data["deploy"]), &deployData); err != nil {
		return fmt.Errorf("error retrieving value for key 'deploy' from configmap %s. %w", KserveConfigMapName, err)
	}
	deployData["defaultDeploymentMode"] = defaultmode
	deployDataBytes, err := json.MarshalIndent(deployData, "", " ")
	if err != nil {
		return fmt.Errorf("could not set values in configmap %s. %w", KserveConfigMapName, err)
	}
	inferenceServiceConfigMap.Data["deploy"] = string(deployDataBytes)

	var ingressData map[string]interface{}
	if err = json.Unmarshal([]byte(inferenceServiceConfigMap.Data["ingress"]), &ingressData); err != nil {
		return fmt.Errorf("error retrieving value for key 'ingress' from configmap %s. %w", KserveConfigMapName, err)
	}
	if defaultmode == RawDeployment {
		ingressData["disableIngressCreation"] = true
	} else {
		ingressData["disableIngressCreation"] = false
	}
	ingressDataBytes, err := json.MarshalIndent(ingressData, "", " ")
	if err != nil {
		return fmt.Errorf("could not set values in configmap %s. %w", KserveConfigMapName, err)
	}
	inferenceServiceConfigMap.Data["ingress"] = string(ingressDataBytes)

	if err = cli.Update(ctx, inferenceServiceConfigMap); err != nil {
		return fmt.Errorf("could not set default deployment mode for Kserve. %w", err)
	}

	return nil
}

func (k *Kserve) configureServerless(instance *dsciv1.DSCInitializationSpec) error {
	switch k.Serving.ManagementState {
	case operatorv1.Unmanaged: // Bring your own CR
		fmt.Println("Serverless CR is not configured by the operator, we won't do anything")

	case operatorv1.Removed: // we remove serving CR
		fmt.Println("existing Serverless CR (owned by operator) will be removed")
		if err := k.removeServerlessFeatures(instance); err != nil {
			return err
		}

	case operatorv1.Managed: // standard workflow to create CR
		switch instance.ServiceMesh.ManagementState {
		case operatorv1.Unmanaged, operatorv1.Removed:
			return fmt.Errorf("ServiceMesh is need to set to 'Managed' in DSCI CR, it is required by KServe serving field")
		}

		serverlessFeatures := feature.ComponentFeaturesHandler(k, instance, k.configureServerlessFeatures())

		if err := serverlessFeatures.Apply(); err != nil {
			return err
		}
	}
	return nil
}

func (k *Kserve) removeServerlessFeatures(instance *dsciv1.DSCInitializationSpec) error {
	serverlessFeatures := feature.ComponentFeaturesHandler(k, instance, k.configureServerlessFeatures())

	return serverlessFeatures.Delete()
}

func checkDependentOperators(cli client.Client) *multierror.Error {
	var multiErr *multierror.Error

	if found, err := deploy.OperatorExists(cli, ServiceMeshOperator); err != nil {
		multiErr = multierror.Append(multiErr, err)
	} else if !found {
		err = fmt.Errorf("operator %s not found. Please install the operator before enabling %s component",
			ServiceMeshOperator, ComponentName)
		multiErr = multierror.Append(multiErr, err)
	}

	if found, err := deploy.OperatorExists(cli, ServerlessOperator); err != nil {
		multiErr = multierror.Append(multiErr, err)
	} else if !found {
		err = fmt.Errorf("operator %s not found. Please install the operator before enabling %s component",
			ServerlessOperator, ComponentName)
		multiErr = multierror.Append(multiErr, err)
	}
	return multiErr
}
