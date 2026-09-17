package v2_test

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	operatorv1 "github.com/openshift/api/operator/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v2"
	dscv3 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v3"
	v2webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v2"
	v3webhook "github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/datasciencecluster/v3"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/webhook/envtestutil"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/envt"

	. "github.com/onsi/gomega"
)

func TestDSCConversionReviewProtocol(t *testing.T) {
	t.Parallel()
	g := NewWithT(t)
	_, env, teardown := envtestutil.SetupEnvAndClient(t,
		[]envt.RegisterWebhooksFn{v2webhook.RegisterWebhooks, v3webhook.RegisterWebhooks},
		nil, envtestutil.DefaultWebhookTimeout)
	t.Cleanup(teardown)

	options := env.Env.WebhookInstallOptions
	roots := x509.NewCertPool()
	g.Expect(roots.AppendCertsFromPEM(options.LocalServingCAData)).To(BeTrue())
	httpClient := &http.Client{
		Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: roots}},
		Timeout:   10 * time.Second,
	}
	url := fmt.Sprintf("https://%s/convert", net.JoinHostPort(options.LocalServingHost, strconv.Itoa(options.LocalServingPort)))

	v2Objects := []*dscv2.DataScienceCluster{
		{TypeMeta: metav1.TypeMeta{APIVersion: dscv2.GroupVersion.String(), Kind: "DataScienceCluster"}, ObjectMeta: metav1.ObjectMeta{Name: "first"}},
		{TypeMeta: metav1.TypeMeta{APIVersion: dscv2.GroupVersion.String(), Kind: "DataScienceCluster"}, ObjectMeta: metav1.ObjectMeta{Name: "second"}},
	}
	v2Objects[0].Spec.Components.Dashboard.ManagementState = operatorv1.Managed
	v2Objects[0].Spec.Components.Dashboard.MaaSConsumerPortal.ManagementState = operatorv1.Managed
	v2Objects[0].Spec.Components.ModelRegistry.ManagementState = operatorv1.Managed
	v2Objects[0].Spec.Components.ModelRegistry.RegistriesNamespace = "model-registry-ns"
	v2Objects[0].Spec.Components.FeastOperator.ManagementState = operatorv1.Managed
	v2Objects[0].Spec.Components.FeastOperator.DataRegistry.ManagementState = operatorv1.Managed
	v2Objects[0].Spec.Components.TrainingOperator.ManagementState = operatorv1.Managed
	v2Objects[0].Spec.Components.LlamaStackOperator.ManagementState = operatorv1.Managed
	v2Objects[0].Status.Components.ModelRegistry.ManagementState = operatorv1.Managed
	v2Objects[0].Status.Conditions = []common.Condition{{Type: "ModelRegistryReady", Status: metav1.ConditionTrue, Reason: "Test"}}
	v2Objects[1].Spec.Components.Workbenches.ManagementState = operatorv1.Removed

	v2Raw := make([]runtime.RawExtension, len(v2Objects))
	for i, object := range v2Objects {
		encoded, err := json.Marshal(object)
		g.Expect(err).NotTo(HaveOccurred())
		v2Raw[i] = runtime.RawExtension{Raw: encoded}
	}

	for _, objects := range [][]runtime.RawExtension{v2Raw[:1], v2Raw} {
		review := postDSCConversionReview(t, httpClient, url, "forward", dscv3.GroupVersion.String(), objects)
		g.Expect(review.Response.Result.Status).To(Equal(metav1.StatusSuccess))
		g.Expect(review.Response.ConvertedObjects).To(HaveLen(len(objects)))
		for i, converted := range review.Response.ConvertedObjects {
			var object dscv3.DataScienceCluster
			g.Expect(json.Unmarshal(converted.Raw, &object)).To(Succeed())
			g.Expect(object.APIVersion).To(Equal(dscv3.GroupVersion.String()))
			g.Expect(object.Name).To(Equal(v2Objects[i].Name))
			if i == 0 {
				g.Expect(object.Spec.Components.Dashboard.Standard.ManagementState).To(Equal(operatorv1.Managed))
				g.Expect(object.Spec.Components.Dashboard.MaaSPortal.ManagementState).To(Equal(operatorv1.Managed))
				g.Expect(object.Spec.Components.AIHub.ApplicationNamespace).To(Equal("model-registry-ns"))
				g.Expect(object.Spec.Components.Data.FeatureStore.ManagementState).To(Equal(operatorv1.Managed))
				g.Expect(object.Spec.Components.Data.DataRegistry.ManagementState).To(Equal(operatorv1.Managed))
				g.Expect(object.Status.Conditions[0].Type).To(Equal("AIHubReady"))
				var wire map[string]any
				g.Expect(json.Unmarshal(converted.Raw, &wire)).To(Succeed())
				components, found, err := unstructured.NestedMap(wire, "spec", "components")
				g.Expect(err).NotTo(HaveOccurred())
				g.Expect(found).To(BeTrue())
				g.Expect(components).NotTo(HaveKey("trainingoperator"))
				g.Expect(components).NotTo(HaveKey("llamastackoperator"))
			} else {
				g.Expect(object.Spec.Components.Workbenches.ManagementState).To(Equal(operatorv1.Removed))
			}
		}
	}

	v3Objects := []*dscv3.DataScienceCluster{
		{TypeMeta: metav1.TypeMeta{APIVersion: dscv3.GroupVersion.String(), Kind: "DataScienceCluster"}, ObjectMeta: metav1.ObjectMeta{Name: "first"}},
		{TypeMeta: metav1.TypeMeta{APIVersion: dscv3.GroupVersion.String(), Kind: "DataScienceCluster"}, ObjectMeta: metav1.ObjectMeta{Name: "second"}},
	}
	v3Objects[0].Spec.Components.Dashboard.Standard.ManagementState = operatorv1.Managed
	v3Objects[0].Spec.Components.Dashboard.MaaSPortal.ManagementState = operatorv1.Managed
	v3Objects[0].Spec.Components.AIHub.ManagementState = operatorv1.Managed
	v3Objects[0].Spec.Components.AIHub.ApplicationNamespace = "aihub-ns"
	v3Objects[0].Spec.Components.Data.FeatureStore.ManagementState = operatorv1.Managed
	v3Objects[0].Spec.Components.Data.DataRegistry.ManagementState = operatorv1.Managed
	v3Objects[0].Status.Conditions = []common.Condition{{Type: "AIHubReady", Status: metav1.ConditionTrue, Reason: "Test"}}
	v3Objects[1].Spec.Components.Workbenches.ManagementState = operatorv1.Removed

	v3Raw := make([]runtime.RawExtension, len(v3Objects))
	for i, object := range v3Objects {
		encoded, err := json.Marshal(object)
		g.Expect(err).NotTo(HaveOccurred())
		v3Raw[i] = runtime.RawExtension{Raw: encoded}
	}
	for _, objects := range [][]runtime.RawExtension{v3Raw[:1], v3Raw} {
		review := postDSCConversionReview(t, httpClient, url, "reverse", dscv2.GroupVersion.String(), objects)
		g.Expect(review.Response.Result.Status).To(Equal(metav1.StatusSuccess))
		g.Expect(review.Response.ConvertedObjects).To(HaveLen(len(objects)))
		for i, converted := range review.Response.ConvertedObjects {
			var object dscv2.DataScienceCluster
			g.Expect(json.Unmarshal(converted.Raw, &object)).To(Succeed())
			g.Expect(object.APIVersion).To(Equal(dscv2.GroupVersion.String()))
			g.Expect(object.Name).To(Equal(v3Objects[i].Name))
			g.Expect(object.Spec.Components.TrainingOperator.ManagementState).To(Equal(operatorv1.Removed))
			g.Expect(object.Spec.Components.LlamaStackOperator.ManagementState).To(Equal(operatorv1.Removed))
			g.Expect(object.Status.Components.TrainingOperator.ManagementState).To(Equal(operatorv1.Removed))
			g.Expect(object.Status.Components.LlamaStackOperator.ManagementState).To(Equal(operatorv1.Removed))
			if i == 0 {
				g.Expect(object.Spec.Components.Dashboard.ManagementState).To(Equal(operatorv1.Managed))
				g.Expect(object.Spec.Components.Dashboard.MaaSConsumerPortal.ManagementState).To(Equal(operatorv1.Managed))
				g.Expect(object.Spec.Components.ModelRegistry.RegistriesNamespace).To(Equal("aihub-ns"))
				g.Expect(object.Spec.Components.FeastOperator.ManagementState).To(Equal(operatorv1.Managed))
				g.Expect(object.Spec.Components.FeastOperator.DataRegistry.ManagementState).To(Equal(operatorv1.Managed))
				g.Expect(object.Status.Conditions[0].Type).To(Equal("ModelRegistryReady"))
			} else {
				g.Expect(object.Spec.Components.Workbenches.ManagementState).To(Equal(operatorv1.Removed))
			}
		}
	}

	unsupported := postDSCConversionReview(t, httpClient, url, "unsupported", dscv3.GroupVersion.Group+"/v4", v2Raw[:1])
	g.Expect(unsupported.Response.Result.Status).To(Equal(metav1.StatusFailure))
	g.Expect(unsupported.Response.ConvertedObjects).To(BeEmpty())

	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewBufferString("{"))
	g.Expect(err).NotTo(HaveOccurred())
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	g.Expect(err).NotTo(HaveOccurred())
	t.Cleanup(func() { g.Expect(response.Body.Close()).To(Succeed()) })
	g.Expect(response.StatusCode).NotTo(Equal(http.StatusOK))
}

func postDSCConversionReview(t *testing.T, httpClient *http.Client, url, uid, desiredVersion string, objects []runtime.RawExtension) apiextensionsv1.ConversionReview {
	t.Helper()
	g := NewWithT(t)
	review := apiextensionsv1.ConversionReview{
		TypeMeta: metav1.TypeMeta{APIVersion: apiextensionsv1.SchemeGroupVersion.String(), Kind: "ConversionReview"},
		Request: &apiextensionsv1.ConversionRequest{
			UID: types.UID(uid), DesiredAPIVersion: desiredVersion, Objects: objects,
		},
	}
	encoded, err := json.Marshal(review)
	g.Expect(err).NotTo(HaveOccurred())
	request, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, bytes.NewReader(encoded))
	g.Expect(err).NotTo(HaveOccurred())
	request.Header.Set("Content-Type", "application/json")
	response, err := httpClient.Do(request)
	g.Expect(err).NotTo(HaveOccurred())
	defer response.Body.Close()
	g.Expect(response.StatusCode).To(Equal(http.StatusOK))
	var converted apiextensionsv1.ConversionReview
	g.Expect(json.NewDecoder(response.Body).Decode(&converted)).To(Succeed())
	g.Expect(converted.APIVersion).To(Equal(apiextensionsv1.SchemeGroupVersion.String()))
	g.Expect(converted.Response).NotTo(BeNil())
	g.Expect(converted.Response.UID).To(Equal(types.UID(uid)))
	return converted
}
