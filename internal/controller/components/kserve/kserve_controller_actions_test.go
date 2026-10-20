//nolint:testpackage
package kserve

import (
	"testing"

	"github.com/onsi/gomega/gstruct"
	operatorv1 "github.com/openshift/api/operator/v1"
	ofapiv1alpha1 "github.com/operator-framework/api/pkg/operators/v1alpha1"
	ofapiv2 "github.com/operator-framework/api/pkg/operators/v2"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	infrav1 "github.com/opendatahub-io/opendatahub-operator/v2/api/infrastructure/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/render/template"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/conditions"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"

	. "github.com/onsi/gomega"
)

func TestCheckPreConditions_ServerlessUnmanaged(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured,
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
		),
	)
}

func TestCheckPreConditions_ServiceMeshUnmanaged(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}
	ks.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Removed,
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(
		MatchError(ContainSubstring(status.ServiceMeshNeedConfiguredMessage)),
	)
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured,
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
		),
	)
}

func TestCheckPreConditions_ServiceMeshManaged_NoOperators(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}
	ks.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(And(
		MatchError(ContainSubstring(ErrServerlessOperatorNotInstalled.Error())),
		MatchError(ContainSubstring(ErrServiceMeshOperatorNotInstalled.Error()))),
	)
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, status.ConditionServingAvailable, err.Error()),
		)),
	)
}

func TestCheckPreConditions_ServiceMeshManaged_NoServerlessOperator(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serviceMeshOperator,
			}},
		),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}
	ks.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(And(
		MatchError(ContainSubstring(ErrServerlessOperatorNotInstalled.Error())),
		MatchError(Not(ContainSubstring(ErrServiceMeshOperatorNotInstalled.Error())))),
	)
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, status.ConditionServingAvailable, err.Error()),
		)),
	)
}

func TestCheckPreConditions_ServiceMeshManaged_NoServiceMeshOperator(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serverlessOperator,
			}},
		),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}
	ks.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(And(
		MatchError(Not(ContainSubstring(ErrServerlessOperatorNotInstalled.Error()))),
		MatchError(ContainSubstring(ErrServiceMeshOperatorNotInstalled.Error()))),
	)
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, status.ConditionServingAvailable, err.Error()),
		)),
	)
}

func TestCheckPreConditions_ServiceMeshManaged_AllOperator(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serviceMeshOperator,
			}},
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serverlessOperator,
			}},
		),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}
	ks.Spec.Serving.ManagementState = operatorv1.Managed

	// Set ServiceMesh condition to True since we're testing operator checks
	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
	}
	dsci.Status.Conditions = []common.Condition{
		{
			Type:   status.CapabilityServiceMesh,
			Status: metav1.ConditionTrue,
			Reason: "ServiceMeshReady",
		},
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionTrue),
		)),
	)
}

func TestCheckPreConditions_ServiceMeshConditionNotTrue(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serviceMeshOperator,
			}},
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serverlessOperator,
			}},
		),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}
	ks.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	// Set ServiceMesh to Managed
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
	}
	// Set ServiceMesh condition to Unknown (not ready)
	dsci.Status.Conditions = []common.Condition{
		{
			Type:   status.CapabilityServiceMesh,
			Status: metav1.ConditionUnknown,
			Reason: "ServiceMeshNotReady",
		},
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rr)
	g.Expect(err).Should(
		MatchError(ContainSubstring(status.ServiceMeshNotReadyMessage)),
	)
	g.Expect(&ks).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .message == "%s"`, status.ConditionServingAvailable, status.ServiceMeshNotReadyMessage),
		)),
	)
}

func TestCheckPreConditions_RawServiceConfig(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ksHeaded := componentApi.Kserve{}
	ksHeaded.Spec.DefaultDeploymentMode = componentApi.RawDeployment
	ksHeaded.Spec.RawDeploymentServiceConfig = componentApi.KserveRawHeaded

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Removed,
	}

	rrHeaded := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ksHeaded,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ksHeaded, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rrHeaded)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(&ksHeaded).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
		)),
	)

	ksHeadless := componentApi.Kserve{}
	ksHeadless.Spec.DefaultDeploymentMode = componentApi.RawDeployment
	ksHeadless.Spec.RawDeploymentServiceConfig = componentApi.KserveRawHeadless

	rrHeadless := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ksHeaded,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ksHeaded, status.ConditionTypeReady),
	}

	err = checkPreConditions(ctx, &rrHeadless)
	g.Expect(err).ShouldNot(HaveOccurred())
	g.Expect(&ksHeaded).Should(
		WithTransform(resources.ToUnstructured, And(
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionServingAvailable, metav1.ConditionFalse),
		)),
	)
}

func TestCleanUpTemplatedResources_withAuthorino(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serviceMeshOperator,
			}},
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serverlessOperator,
			}},
			&ofapiv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{
				Name: authorinoOperator,
			}},
		),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	ksHeaded := componentApi.Kserve{}
	ksHeaded.Spec.DefaultDeploymentMode = componentApi.Serverless
	ksHeaded.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
		ControlPlane: infrav1.ControlPlaneSpec{
			Namespace: "istio-system",
		},
		Auth: infrav1.AuthSpec{
			Namespace: "custom-authorino",
		},
	}

	rrHeaded := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ksHeaded,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ksHeaded, status.ConditionTypeReady),
	}

	err = addTemplateFiles(ctx, &rrHeaded)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = template.NewAction(
		template.WithDataFn(getTemplateData),
	)(ctx, &rrHeaded)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cleanUpTemplatedResources(ctx, &rrHeaded)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(rrHeaded.Resources).Should(
		And(
			HaveLen(12),
			ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Object": And(
					HaveKeyWithValue("kind", gvk.AuthorizationPolicy.Kind),
				),
			})),
			ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Object": And(
					HaveKeyWithValue("kind", gvk.EnvoyFilter.Kind),
				),
			})),
			ContainElement(WithTransform(func(obj unstructured.Unstructured) map[string]any {
				return obj.Object
			}, And(
				jq.Match(`.metadata.name == "kserve-authorino-ext-authz"`),
				jq.Match(`.metadata.namespace == "istio-system"`),
				jq.Match(`.metadata.labels["platform.opendatahub.io/dependency"] == "servicemesh"`),
				jq.Match(`.spec.workloadSelector.labels.component == "predictor"`),
				jq.Match(`.spec.configPatches[0].applyTo == "CLUSTER"`),
				jq.Match(`.spec.configPatches[0].match.context == "SIDECAR_OUTBOUND"`),
				jq.Match(`.spec.configPatches[0].patch.operation == "ADD"`),
				jq.Match(`.spec.configPatches[0].patch.value.name == "odhai-authorino-ext-authz"`),
				jq.Match(`.spec.configPatches[0].patch.value.type == "STRICT_DNS"`),
				jq.Match(`.spec.configPatches[0].patch.value.load_assignment.cluster_name == "odhai-authorino-ext-authz"`),
				jq.Match(`.spec.configPatches[0].patch.value.load_assignment.endpoints[0].lb_endpoints[0].endpoint.address.socket_address.address == "authorino-authorino-authorization.custom-authorino.svc.cluster.local"`),
				jq.Match(`.spec.configPatches[0].patch.value.load_assignment.endpoints[0].lb_endpoints[0].endpoint.address.socket_address.port_value == 50051`),
				jq.Match(`.spec.configPatches[0].patch.value.http2_protocol_options == {}`),
				jq.Match(`.spec.configPatches[0].patch.value.transport_socket.typed_config.common_tls_context.tls_certificate_sds_secret_configs[0].name == "default"`),
				jq.Match(`.spec.configPatches[0].patch.value.transport_socket.typed_config.common_tls_context.combined_validation_context.validation_context_sds_secret_config.name == "ROOTCA"`),
				jq.Match(`.spec.configPatches[0].patch.value.transport_socket.typed_config.common_tls_context.tls_certificate_sds_secret_configs[0].sds_config.api_config_source.grpc_services[0].envoy_grpc.cluster_name == "sds-grpc"`),
				jq.Match(`.spec.configPatches[0].patch.value.transport_socket.typed_config.common_tls_context.tls_certificate_sds_secret_configs[0].sds_config.api_config_source.transport_api_version == "V3"`),
				jq.Match(`.spec.configPatches[0].patch.value.transport_socket.typed_config.common_tls_context.tls_certificate_sds_secret_configs[0].sds_config.resource_api_version == "V3"`),
				jq.Match(`.spec.configPatches[0].patch.value.transport_socket.typed_config.common_tls_context.combined_validation_context.validation_context_sds_secret_config.sds_config.api_config_source.grpc_services[0].envoy_grpc.cluster_name == "sds-grpc"`),
				jq.Match(`.spec.configPatches[0].patch.value.transport_socket.typed_config.common_tls_context.combined_validation_context.validation_context_sds_secret_config.sds_config.api_config_source.transport_api_version == "V3"`),
				jq.Match(`.spec.configPatches[0].patch.value.transport_socket.typed_config.common_tls_context.combined_validation_context.validation_context_sds_secret_config.sds_config.resource_api_version == "V3"`),
				jq.Match(`.spec.configPatches[0].patch.value.transport_socket.typed_config.sni == "outbound_.50051_._.authorino-authorino-authorization.custom-authorino.svc.cluster.local"`),
				jq.Match(`.spec.configPatches[0].patch.value.transport_socket.typed_config.common_tls_context.combined_validation_context.default_validation_context.match_typed_subject_alt_names[0].matcher.exact == "spiffe://cluster.local/ns/custom-authorino/sa/authorino-authorino"`),
				jq.Match(`.spec.configPatches[1].applyTo == "HTTP_FILTER"`),
				jq.Match(`.spec.configPatches[1].match.context == "SIDECAR_INBOUND"`),
				jq.Match(`.spec.configPatches[1].match.listener.filterChain.filter.subFilter.name == "envoy.filters.http.ext_authz"`),
				jq.Match(`.spec.configPatches[1].patch.operation == "MERGE"`),
				jq.Match(`.spec.configPatches[1].patch.value.typed_config.grpc_service.envoy_grpc.cluster_name == "odhai-authorino-ext-authz"`),
			))),
		),
	)
}

func TestCleanUpTemplatedResources_withoutAuthorino(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serviceMeshOperator,
			}},
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serverlessOperator,
			}},
		),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	ksHeaded := componentApi.Kserve{}
	ksHeaded.Spec.DefaultDeploymentMode = componentApi.Serverless
	ksHeaded.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Managed,
	}

	rrHeaded := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ksHeaded,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ksHeaded, status.ConditionTypeReady),
	}

	err = addTemplateFiles(ctx, &rrHeaded)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = template.NewAction(
		template.WithDataFn(getTemplateData),
	)(ctx, &rrHeaded)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = cleanUpTemplatedResources(ctx, &rrHeaded)
	g.Expect(err).ShouldNot(HaveOccurred())

	g.Expect(rrHeaded.Resources).Should(
		And(
			HaveLen(7),
			Not(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Object": And(
					HaveKeyWithValue("kind", gvk.AuthorizationPolicy.Kind),
				),
			}))),
			Not(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
				"Object": And(
					HaveKeyWithValue("kind", gvk.EnvoyFilter.Kind),
				),
			}))),
		),
	)
}

func TestCleanUpTemplatedResources_withServiceMeshUnmanaged(t *testing.T) {
	ctx := t.Context()
	g := NewWithT(t)

	cli, err := fakeclient.New(
		fakeclient.WithObjects(
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serviceMeshOperator,
			}},
			&ofapiv2.OperatorCondition{ObjectMeta: metav1.ObjectMeta{
				Name: serverlessOperator,
			}},
			&ofapiv1alpha1.Subscription{ObjectMeta: metav1.ObjectMeta{
				Name: authorinoOperator,
			}},
		),
	)

	g.Expect(err).ShouldNot(HaveOccurred())

	ks := componentApi.Kserve{}
	ks.Spec.DefaultDeploymentMode = componentApi.Serverless
	ks.Spec.Serving.ManagementState = operatorv1.Managed

	dsci := dsciv1.DSCInitialization{}
	dsci.Spec.ServiceMesh = &infrav1.ServiceMeshSpec{
		ManagementState: operatorv1.Unmanaged,
	}

	rr := types.ReconciliationRequest{
		Client:     cli,
		Instance:   &ks,
		DSCI:       &dsci,
		Conditions: conditions.NewManager(&ks, status.ConditionTypeReady),
	}

	g.Expect(addTemplateFiles(ctx, &rr)).ShouldNot(HaveOccurred())
	g.Expect(template.NewAction(template.WithDataFn(getTemplateData))(ctx, &rr)).ShouldNot(HaveOccurred())
	g.Expect(cleanUpTemplatedResources(ctx, &rr)).ShouldNot(HaveOccurred())

	g.Expect(rr.Resources).Should(And(
		HaveLen(7),
		Not(ContainElement(gstruct.MatchFields(gstruct.IgnoreExtras, gstruct.Fields{
			"Object": And(HaveKeyWithValue("kind", gvk.EnvoyFilter.Kind)),
		}))),
	))
}
