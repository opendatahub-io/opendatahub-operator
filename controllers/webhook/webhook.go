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

package webhook

import (
	"context"
	"fmt"
	"net/http"

	operatorv1 "github.com/openshift/api/operator/v1"
	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/components/modelregistry"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

var log = ctrl.Log.WithName("rhoai-controller-webhook")

//+kubebuilder:webhook:path=/validate-opendatahub-io-v1,mutating=false,failurePolicy=fail,sideEffects=None,groups=datasciencecluster.opendatahub.io;dscinitialization.opendatahub.io,resources=datascienceclusters;dscinitializations,verbs=create;delete,versions=v1,name=operator.opendatahub.io,admissionReviewVersions=v1
//nolint:lll

type OpenDataHubValidatingWebhook struct {
	Client  client.Client
	Decoder *admission.Decoder
}

func (w *OpenDataHubValidatingWebhook) SetupWithManager(mgr ctrl.Manager) {
	hookServer := mgr.GetWebhookServer()
	odhWebhook := &webhook.Admission{
		Handler: w,
	}
	hookServer.Register("/validate-opendatahub-io-v1", odhWebhook)
}

func countObjects(ctx context.Context, cli client.Client, gvk schema.GroupVersionKind) (int, error) {
	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)

	if err := cli.List(ctx, list); err != nil {
		return 0, err
	}

	return len(list.Items), nil
}

func denyCountGtZero(ctx context.Context, cli client.Client, gvk schema.GroupVersionKind, msg string) admission.Response {
	count, err := countObjects(ctx, cli, gvk)
	if err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	if count > 0 {
		return admission.Denied(msg)
	}

	return admission.Allowed("")
}

func (w *OpenDataHubValidatingWebhook) checkDupCreation(ctx context.Context, req admission.Request) admission.Response {
	switch req.Kind.Kind {
	case "DataScienceCluster", "DSCInitialization":
	default:
		log.Info("Got wrong kind", "kind", req.Kind.Kind)
		return admission.Errored(http.StatusBadRequest, nil)
	}

	gvk := schema.GroupVersionKind{
		Group:   req.Kind.Group,
		Version: req.Kind.Version,
		Kind:    req.Kind.Kind,
	}

	// if count == 1 now creation of #2 is being handled
	return denyCountGtZero(ctx, w.Client, gvk,
		fmt.Sprintf("Only one instance of %s object is allowed", req.Kind.Kind))
}

func (w *OpenDataHubValidatingWebhook) checkDeletion(ctx context.Context, req admission.Request) admission.Response {
	if req.Kind.Kind == "DataScienceCluster" {
		return admission.Allowed("")
	}

	// Restrict deletion of DSCI if DSC exists
	return denyCountGtZero(ctx, w.Client, gvk.DataScienceCluster,
		fmt.Sprintln("Cannot delete DSCI object when DSC object still exists"))
}

func (w *OpenDataHubValidatingWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var resp admission.Response
	resp.Allowed = true // initialize Allowed to be true in case Operation falls into "default" case

	switch req.Operation {
	case admissionv1.Create:
		resp = w.checkDupCreation(ctx, req)
	case admissionv1.Delete:
		resp = w.checkDeletion(ctx, req)
	default: // for other operations by default it is admission.Allowed("")
		// no-op
	}
	if !resp.Allowed {
		return resp
	}

	return admission.Allowed(fmt.Sprintf("Operation %s on %s allowed", req.Operation, req.Kind.Kind))
}

//+kubebuilder:webhook:path=/mutate-opendatahub-io-v1,mutating=true,failurePolicy=fail,sideEffects=None,groups=datasciencecluster.opendatahub.io,resources=datascienceclusters,verbs=create;update,versions=v1,name=mutate.operator.opendatahub.io,admissionReviewVersions=v1
//nolint:lll

// OpenDataHubMutatingWebhook is a mutating webhook
// It currently only sets defaults for modelregiestry in datascienceclusters.
type OpenDataHubMutatingWebhook struct {
	Client  client.Client
	Decoder *admission.Decoder
}

func (w *OpenDataHubMutatingWebhook) SetupWithManager(mgr ctrl.Manager) {
	hookServer := mgr.GetWebhookServer()
	odhWebhook := &webhook.Admission{
		Handler: w,
	}
	hookServer.Register("/mutate-opendatahub-io-v1", odhWebhook)
}

func (w *OpenDataHubMutatingWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var resp admission.Response
	resp.Allowed = true // initialize Allowed to be true in case Operation falls into "default" case

	dsc := &dscv1.DataScienceCluster{}
	err := w.Decoder.Decode(req, dsc)
	if err != nil {
		return admission.Denied(fmt.Sprintf("failed to decode request body: %s", err))
	}
	resp = w.setDSCDefaults(ctx, dsc)
	return resp
}

func (w *OpenDataHubMutatingWebhook) setDSCDefaults(_ context.Context, dsc *dscv1.DataScienceCluster) admission.Response {
	// set default registriesNamespace if empty
	if len(dsc.Spec.Components.ModelRegistry.RegistriesNamespace) == 0 && dsc.Spec.Components.ModelRegistry.ManagementState == operatorv1.Managed {
		return admission.Patched("Property registriesNamespace set to default value", webhook.JSONPatchOp{
			Operation: "replace",
			Path:      "spec.components.modelregistry.registriesNamespace",
			Value:     modelregistry.DefaultModelRegistriesNamespace,
		})
	}
	return admission.Allowed("")
}
