//nolint:testpackage
package datasciencecluster

import (
	"context"
	"errors"
	"testing"

	"github.com/go-logr/logr/funcr"
	operatorv1 "github.com/openshift/api/operator/v1"
	k8serr "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/fake"
	"sigs.k8s.io/controller-runtime/pkg/client/interceptor"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/api/components/v1alpha1"
	configApi "github.com/opendatahub-io/opendatahub-operator/v2/api/config/v1alpha2"
	dsciv2 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v2"
	serviceApi "github.com/opendatahub-io/opendatahub-operator/v2/api/services/v1alpha1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/aihub"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/dashboard"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/modules/workbenches"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	conditionstest "github.com/opendatahub-io/opendatahub-operator/v2/internal/testutil/conditions"
	odherrors "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/actions/errors"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/dag"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/provision"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/types"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/fakeclient"

	. "github.com/onsi/gomega"
)

func logCapturingContext(t *testing.T) (context.Context, *[]string) {
	t.Helper()
	var logged []string
	logger := funcr.New(func(_, args string) {
		logged = append(logged, args)
	}, funcr.Options{})
	return logf.IntoContext(t.Context(), logger), &logged
}

type mockModuleHandler struct {
	modules.BaseHandler

	deleteErr error
}

func (m *mockModuleHandler) IsEnabled(_ *configApi.PlatformModules) bool { return false }

func (m *mockModuleHandler) BuildModuleCR(_ context.Context, _ client.Client, _ *modules.DSCContext, _ *modules.ModuleCRConfig) (*unstructured.Unstructured, error) {
	return nil, nil
}
func (m *mockModuleHandler) DeleteModuleCR(_ context.Context, _ client.Client) error {
	return m.deleteErr
}

var _ modules.ModuleHandler = (*mockModuleHandler)(nil)

func TestCheckPreConditionsStopsDuringUninstall(t *testing.T) {
	g := NewWithT(t)

	dsc := newDSC()
	dsci := &dsciv2.DSCInitialization{}

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsci))
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &types.ReconciliationRequest{
		Client:   cli,
		Instance: dsc,
	}
	rr.Conditions = conditionstest.NewManager(
		dsc,
		status.ConditionTypeReady,
		status.ConditionTypeComponentsReady,
		status.ConditionTypeModulesReady,
	)
	dsc.Status.Conditions = []common.Condition{{
		Type:    status.ConditionTypeReady,
		Status:  metav1.ConditionFalse,
		Reason:  status.UninstallInProgressReason,
		Message: status.UninstallInProgressMessage,
	}}

	err = checkPreConditions(t.Context(), rr)

	var stopErr odherrors.StopError
	g.Expect(errors.As(err, &stopErr)).To(BeTrue())
	g.Expect(stopErr.RequeueAfter()).To(BeNumerically(">", 0))
	rr.Conditions.CleanupStaleConditions()
	rr.Conditions.RecomputeHappiness()

	ready := rr.Conditions.GetTopLevelCondition()
	g.Expect(ready.Status).To(Equal(metav1.ConditionFalse))
	g.Expect(ready.Reason).To(Equal(status.UninstallInProgressReason))
	g.Expect(ready.Message).To(Equal(status.UninstallInProgressMessage))
}

func TestCheckPreConditionsAllowsNonUninstallReadyCondition(t *testing.T) {
	g := NewWithT(t)

	dsc := newDSC()
	dsc.Status.Conditions = []common.Condition{{
		Type:   status.ConditionTypeReady,
		Status: metav1.ConditionFalse,
		Reason: status.NotReadyReason,
	}}
	dsci := &dsciv2.DSCInitialization{}

	cli, err := fakeclient.New(fakeclient.WithObjects(dsc, dsci))
	g.Expect(err).ShouldNot(HaveOccurred())

	rr := &types.ReconciliationRequest{
		Client:   cli,
		Instance: dsc,
	}

	g.Expect(checkPreConditions(t.Context(), rr)).Should(Succeed())
}

type recordingModuleHandler struct {
	modules.ModuleHandler

	config *modules.ModuleCRConfig
}

// DSC provisioning uses projected management states, independently of handler policy.
func (h *recordingModuleHandler) IsEnabled(_ *configApi.PlatformModules) bool { return false }

func (h *recordingModuleHandler) BuildModuleCR(ctx context.Context, cli client.Client, dsc *modules.DSCContext, cfg *modules.ModuleCRConfig) (*unstructured.Unstructured, error) {
	h.config = cfg
	return h.ModuleHandler.BuildModuleCR(ctx, cli, dsc, cfg)
}

func TestProvisionModuleCRsGatewayConfig(t *testing.T) {
	readErr := errors.New("gateway read failed")
	for _, tc := range []struct {
		name    string
		present bool
		readErr error
	}{
		{name: "present", present: true},
		{name: "missing"},
		{name: "read failure", present: true, readErr: readErr},
	} {
		t.Run(tc.name, func(t *testing.T) {
			g := NewWithT(t)
			reg := &modules.Registry{}
			handlers := []*recordingModuleHandler{
				{ModuleHandler: workbenches.NewHandler()},
				{ModuleHandler: dashboard.NewHandler()},
				{ModuleHandler: aihub.NewHandler()},
			}
			for _, h := range handlers {
				reg.Add(h)
			}
			dsc := newDSC()
			dsc.Spec.Components.Workbenches.ManagementState = operatorv1.Managed
			dsc.Spec.Components.Dashboard.Standard.ManagementState = operatorv1.Managed
			dsc.Spec.Components.AIHub.ManagementState = operatorv1.Removed
			objects := []client.Object{&dsciv2.DSCInitialization{
				ObjectMeta: metav1.ObjectMeta{Name: "default-dsci"},
				Spec:       dsciv2.DSCInitializationSpec{ApplicationsNamespace: "test-apps"},
			}}
			gatewayConfig := &serviceApi.GatewayConfig{
				ObjectMeta: metav1.ObjectMeta{Name: serviceApi.GatewayConfigName},
				Status:     serviceApi.GatewayConfigStatus{Domain: "apps.example.com"},
			}
			if tc.present {
				objects = append(objects, gatewayConfig)
			}
			gatewayReads := 0
			var gatewaySnapshot *serviceApi.GatewayConfig
			cli, err := fakeclient.New(fakeclient.WithObjects(objects...),
				fakeclient.WithInterceptorFuncs(interceptor.Funcs{
					Get: func(ctx context.Context, cli client.WithWatch, key client.ObjectKey, obj client.Object, opts ...client.GetOption) error {
						if config, ok := obj.(*serviceApi.GatewayConfig); ok {
							gatewayReads++
							gatewaySnapshot = config
							if tc.readErr != nil {
								return tc.readErr
							}
						}
						return cli.Get(ctx, key, obj, opts...)
					},
				}))
			g.Expect(err).NotTo(HaveOccurred())
			rr := &types.ReconciliationRequest{Instance: dsc, Client: cli}
			err = provisionModuleCRsWith(t.Context(), rr, reg)
			g.Expect(gatewayReads).To(Equal(1))
			if !tc.present || tc.readErr != nil {
				g.Expect(err).To(MatchError(ContainSubstring("get GatewayConfig for module CRs")))
				if tc.readErr != nil {
					g.Expect(errors.Is(err, tc.readErr)).To(BeTrue())
				} else {
					g.Expect(k8serr.IsNotFound(err)).To(BeTrue())
				}
				g.Expect(rr.Resources).To(BeEmpty())
				for _, h := range handlers {
					g.Expect(h.config).To(BeNil())
				}
				return
			}
			g.Expect(err).NotTo(HaveOccurred())
			g.Expect(rr.Resources).To(HaveLen(2))
			g.Expect(handlers[0].config).NotTo(BeNil())
			g.Expect(handlers[0].config.GatewayConfig).To(BeIdenticalTo(gatewaySnapshot))
			g.Expect(handlers[1].config).To(BeIdenticalTo(handlers[0].config))
			g.Expect(handlers[2].config).To(BeNil())
		})
	}
}

func TestProvisionComponentsErrorLogs(t *testing.T) {
	tests := []struct {
		name          string
		handler       *mockHandler
		newClient     func() client.Client
		wantMsg       string
		wantComponent string
	}{
		{
			name:          "NewCRObject failure is logged with structured fields",
			handler:       &mockHandler{name: "newcr-fail", enabled: true, newCRErr: errors.New("boom")},
			newClient:     func() client.Client { return nil },
			wantMsg:       "NewCRObject failed",
			wantComponent: "newcr-fail",
		},
		{
			name:    "AddResources failure is logged with structured fields",
			handler: &mockHandler{name: "addresources-fail", enabled: true, newCRObj: &componentApi.Kueue{}},
			// An empty scheme cannot resolve the returned object's GVK, so
			// rr.AddResources fails and provisionComponents logs the error.
			newClient:     func() client.Client { return fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build() },
			wantMsg:       "AddResources failed",
			wantComponent: "addresources-fail",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			g := NewWithT(t)
			dsc := newDSC()

			ctx, logged := logCapturingContext(t)
			rr := &types.ReconciliationRequest{
				Instance:   dsc,
				Client:     tt.newClient(),
				Conditions: conditionstest.NewManager(dsc, status.ConditionTypeComponentsReady),
			}

			err := provisionComponentsWith(ctx, rr, newRegistry(tt.handler))

			g.Expect(err).To(HaveOccurred())
			g.Expect(*logged).To(ContainElement(And(
				ContainSubstring(`"msg"="`+tt.wantMsg+`"`),
				ContainSubstring(`"component"="`+tt.wantComponent+`"`),
			)))
		})
	}
}

func TestProvisionComponentsAggregatesFailedComponents(t *testing.T) {
	g := NewWithT(t)
	dsc := newDSC()

	handlers := []*mockHandler{
		{name: "agg-newcr-fail", enabled: true, newCRErr: errors.New("boom")},
		{name: "agg-addresources-fail", enabled: true, newCRObj: &componentApi.Kueue{}},
	}

	ctx, logged := logCapturingContext(t)

	rr := &types.ReconciliationRequest{
		Instance:   dsc,
		Client:     fake.NewClientBuilder().WithScheme(runtime.NewScheme()).Build(),
		Conditions: conditionstest.NewManager(dsc, status.ConditionTypeComponentsReady),
	}

	err := provisionComponentsWith(ctx, rr, newRegistry(handlers[0], handlers[1]))

	g.Expect(err).To(HaveOccurred())
	g.Expect(err.Error()).To(And(
		ContainSubstring("agg-newcr-fail"),
		ContainSubstring("agg-addresources-fail"),
	))
	g.Expect(*logged).To(ContainElement(ContainSubstring(`"component"="agg-newcr-fail"`)))
	g.Expect(*logged).To(ContainElement(ContainSubstring(`"component"="agg-addresources-fail"`)))
}

func TestCleanupDisabledComponentsLogsDeleteFailure(t *testing.T) {
	g := NewWithT(t)
	dsc := newDSC()

	const name = "delete-fail"

	compReg := newRegistry(&mockHandler{name: name, enabled: false})
	provReg := provision.NewRegistry()
	provReg.Add(name, provision.KindComponent, dag.RL(10))

	cli := fake.NewClientBuilder().
		WithInterceptorFuncs(interceptor.Funcs{
			List: func(_ context.Context, _ client.WithWatch, _ client.ObjectList, _ ...client.ListOption) error {
				return errors.New("list failed")
			},
		}).
		Build()

	ctx, logged := logCapturingContext(t)
	rr := &types.ReconciliationRequest{Instance: dsc, Client: cli}

	err := cleanupDisabledComponentsWith(ctx, rr, compReg, provReg)

	g.Expect(err).To(HaveOccurred())
	g.Expect(*logged).To(ContainElement(And(
		ContainSubstring(`"msg"="failed to delete component CR"`),
		ContainSubstring(`"component"="`+name+`"`),
	)))
}

func TestCleanupDisabledModuleCRsLogsDeleteFailure(t *testing.T) {
	g := NewWithT(t)
	dsc := newDSC()

	const name = "module-delete-fail"
	mod := &mockModuleHandler{deleteErr: errors.New("delete module cr failed")}
	mod.Config = modules.ModuleConfig{
		Name:   name,
		CRName: "default",
		GVK:    schema.GroupVersionKind{Group: "test.io", Version: "v1", Kind: "MockModule"},
	}

	modReg := &modules.Registry{}
	modReg.Add(mod)
	provReg := provision.NewRegistry()
	provReg.Add(name, provision.KindModule, dag.RL(20))

	cli, err := fakeclient.New()
	g.Expect(err).ShouldNot(HaveOccurred())

	ctx, logged := logCapturingContext(t)

	rr := &types.ReconciliationRequest{Instance: dsc, Client: cli}

	err = cleanupDisabledModuleCRsWith(ctx, rr, modReg, provReg)

	g.Expect(err).To(HaveOccurred())
	g.Expect(*logged).To(ContainElement(And(
		ContainSubstring(`"msg"="DeleteModuleCR failed"`),
		ContainSubstring(`"module"="`+name+`"`),
	)))
}
