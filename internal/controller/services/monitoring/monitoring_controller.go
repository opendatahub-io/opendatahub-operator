/*
Copyright 2023.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package monitoring

import (
	"context"
	"errors"
	"fmt"

	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	ctrl "sigs.k8s.io/controller-runtime"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	sr "github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/services/registry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/deploy"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/status/deployments"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/handlers"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/predicates/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/reconciler"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
)

//nolint:gochecknoinits
func init() {
	sr.Add(&serviceHandler{})
}

type serviceHandler struct {
}

func (h *serviceHandler) Init(_ common.Platform) error {
	return nil
}

func (h *serviceHandler) GetName() string {
	return ServiceName
}

func (h *serviceHandler) GetManagementState(platform common.Platform) operatorv1.ManagementState {
	if platform != cluster.ManagedRhoai {
		return operatorv1.Unmanaged
	}

	return operatorv1.Managed
}

func (h *serviceHandler) NewReconciler(ctx context.Context, mgr ctrl.Manager) error {
	_, err := reconciler.ReconcilerFor(mgr, &serviceApi.Monitoring{}).
		// operands - watched
		//
		// By default the Watches functions adds:
		// - an event handler mapping to a cluster scope resource identified by the
		//   components.platform.opendatahub.io/part-of annotation
		// - a predicate that check for generation change for Delete/Updates events
		//   for to objects that have the label components.platform.opendatahub.io/part-of
		// or services.platform.opendatahub.io/part-of set to the current owner
		//
		Watches(&dscv1.DataScienceCluster{}, reconciler.WithEventHandler(handlers.ToNamed(serviceApi.MonitoringInstanceName)),
			reconciler.WithPredicates(resources.DSCComponentUpdatePredicate)).
		// actions
		WithAction(deployments.NewAction(
			deployments.InNamespaceFn(func(_ context.Context, rr *types.ReconciliationRequest) (string, error) {
				m, ok := rr.Instance.(*serviceApi.Monitoring)
				if !ok {
					return "", errors.New("instance is not of type *services.Monitoring")
				}

				return m.Spec.Namespace, nil
			}),
		)).
		WithAction(func(_ context.Context, rr *types.ReconciliationRequest) error {
			m, ok := rr.Instance.(*serviceApi.Monitoring)
			if !ok {
				return errors.New("instance is not of type *services.Monitoring")
			}
			return deployTempo(ctx, rr, rr.DSCI.Spec.Monitoring.Traces, m.Spec.Namespace)
		}).
		WithAction(initialize).
		WithAction(updatePrometheusConfigMap).
		WithAction(deploy.NewAction(
			deploy.WithCache(),
		)).
		Build(ctx)

	if err != nil {
		return fmt.Errorf("could not create the monitoring controller: %w", err)
	}

	return nil
}

func deployTempo(ctx context.Context, rr *types.ReconciliationRequest, traces *serviceApi.Traces, namespace string) error {
	if traces == nil {
		// Ensure Tempo instance is absent
		return removeTempoInstance(ctx, rr, namespace)
	}

	if traces.Storage.Backend == "pv" {
		return deployTempoMonolithic(rr, traces, namespace)
	}
	return deployTempoStack(rr, traces, namespace)
}

func deployTempoMonolithic(rr *types.ReconciliationRequest, traces *serviceApi.Traces, namespace string) error {
	storage := map[string]interface{}{
		"backend": traces.Storage.Backend,
	}

	if traces.Storage.Size != "" {
		storage["size"] = traces.Storage.Size
	}

	tempo := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "tempo.grafana.com/v1alpha1",
			"kind":       "TempoMonolithic",
			"metadata": map[string]interface{}{
				"name":      "tempo",
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"multitenancy": map[string]interface{}{
					"enabled": true, // Required for OpenShift
				},
				"storage": map[string]interface{}{
					"traces": storage,
				},
			},
		},
	}

	if err := rr.AddResources(tempo); err != nil {
		return errors.New("failed to deploy TempoMonolithic")
	}
	return nil
}

func deployTempoStack(rr *types.ReconciliationRequest, traces *serviceApi.Traces, namespace string) error {
	tempo := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "tempo.grafana.com/v1alpha1",
			"kind":       "TempoStack",
			"metadata": map[string]interface{}{
				"name":      "tempo",
				"namespace": namespace,
			},
			"spec": map[string]interface{}{
				"tenants": map[string]interface{}{
					"mode": "openshift",
				},
				"storage": map[string]interface{}{
					"secret": map[string]interface{}{
						"name": traces.Storage.Secret,
						"type": traces.Storage.Backend,
					},
				},
				"template": map[string]interface{}{
					"gateway": map[string]interface{}{
						"enabled": true, // Required for OpenShift mode
					},
				},
			},
		},
	}

	if err := rr.AddResources(tempo); err != nil {
		return errors.New("failed to deploy TempoStack")
	}
	return nil
}

func removeTempoInstance(ctx context.Context, rr *types.ReconciliationRequest, namespace string) error {
	// Delete TempoMonolithic if exists
	mono := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "tempo.grafana.com/v1alpha1",
			"kind":       "TempoMonolithic",
		},
	}
	mono.SetName("tempo")
	mono.SetNamespace(namespace)

	if err := rr.Client.Delete(ctx, mono); err != nil && !k8serr.IsNotFound(err) {
		return errors.New("failed to delete TempoMonolithic")
	}

	// Delete TempoStack if exists
	stack := &unstructured.Unstructured{
		Object: map[string]interface{}{
			"apiVersion": "tempo.grafana.com/v1alpha1",
			"kind":       "TempoStack",
		},
	}
	stack.SetName("tempo")
	stack.SetNamespace(namespace)

	if err := rr.Client.Delete(ctx, stack); err != nil && !k8serr.IsNotFound(err) {
		return errors.New("failed to delete TempoStack")
	}

	return nil
}
