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

func (w *OpenDataHubWebhook) Handle(ctx context.Context, req admission.Request) admission.Response {
	return admission.ValidationResponse(true, "")
}
