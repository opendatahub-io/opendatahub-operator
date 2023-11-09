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

	admissionv1 "k8s.io/api/admission/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/webhook"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"
)

var log = ctrl.Log.WithName("odh-controller-webhook")

//+kubebuilder:webhook:path=/validate-opendatahub-io-v1,mutating=false,failurePolicy=fail,sideEffects=None,groups=datasciencecluster.opendatahub.io;dscinitialization.opendatahub.io,resources=datascienceclusters;dscinitializations,verbs=create;update,versions=v1,name=operator.opendatahub.io,admissionReviewVersions=v1
//nolint:lll

type OpenDataHubWebhook struct {
	client  client.Client
	decoder *admission.Decoder
}

func (w *OpenDataHubWebhook) SetupWithManager(mgr ctrl.Manager) {
	hookServer := mgr.GetWebhookServer()
	odhWebhook := &webhook.Admission{
		Handler: w,
	}
	hookServer.Register("/validate-opendatahub-io-v1", odhWebhook)
}

func (w *OpenDataHubWebhook) InjectDecoder(d *admission.Decoder) error {
	w.decoder = d
	return nil
}

func (w *OpenDataHubWebhook) InjectClient(c client.Client) error {
	w.client = c
	return nil
}

func (w *OpenDataHubWebhook) checkDupCreation(ctx context.Context, req admission.Request) admission.Response {
	if req.Operation != admissionv1.Create {
		return admission.Allowed(fmt.Sprintf("duplication check: skipping %v request", req.Operation))
	}

	switch req.Kind.Kind {
	case "DataScienceCluster":
	case "DSCInitialization":
	default:
		log.Info("Got wrong kind", "kind", req.Kind.Kind)
		return admission.Errored(http.StatusBadRequest, nil)
	}

	gvk := schema.GroupVersionKind{
		Group:   req.Kind.Group,
		Version: req.Kind.Version,
		Kind:    req.Kind.Kind,
	}

	list := &unstructured.UnstructuredList{}
	list.SetGroupVersionKind(gvk)

	if err := w.client.List(ctx, list); err != nil {
		return admission.Errored(http.StatusBadRequest, err)
	}

	// if len == 1 now creation of #2 is being handled
	if len(list.Items) > 0 {
		return admission.Denied(fmt.Sprintf("Only one instance of %s object is allowed", req.Kind.Kind))
	}

	return admission.Allowed(fmt.Sprintf("%s duplication check passed", req.Kind.Kind))
}

func (w *OpenDataHubWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	var resp admission.Response

	// Handle only Create and Update
	if req.Operation == admissionv1.Delete || req.Operation == admissionv1.Connect {
		msg := fmt.Sprintf("ODH skipping %v request", req.Operation)
		log.Info(msg)
		return admission.Allowed(msg)
	}

	resp = w.checkDupCreation(ctx, req)
	if !resp.Allowed {
		return resp
	}

	return admission.Allowed(fmt.Sprintf("%s allowed", req.Kind.Kind))
}
