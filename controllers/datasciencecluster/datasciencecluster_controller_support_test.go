//nolint:testpackage
package datasciencecluster

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"testing"

	operatorv1 "github.com/openshift/api/operator/v1"
	conditionsv1 "github.com/openshift/custom-resource-status/conditions/v1"
	"github.com/stretchr/testify/mock"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"

	"github.com/opendatahub-io/opendatahub-operator/v2/apis/common"
	componentApi "github.com/opendatahub-io/opendatahub-operator/v2/apis/components/v1alpha1"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/controllers/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	cr "github.com/opendatahub-io/opendatahub-operator/v2/pkg/componentsregistry"
	odhClient "github.com/opendatahub-io/opendatahub-operator/v2/pkg/controller/client"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/tests/envtestutil"

	. "github.com/onsi/gomega"
	. "github.com/onsi/gomega/gstruct"
)

func TestSetAvailability(t *testing.T) {
	g := NewGomegaWithT(t)

	instance := &dscv1.DataScienceCluster{}

	// Case 1: When there is no error (err == nil)
	t.Run("No error sets ConditionTrue", func(t *testing.T) {
		result := setAvailability(instance, nil)

		g.Expect(result).To(BeTrue())
		g.Expect(instance.Status.Conditions).To(HaveLen(1))

		g.Expect(instance.Status.Conditions[0]).To(MatchFields(IgnoreExtras, Fields{
			"Type":    Equal(conditionsv1.ConditionAvailable),
			"Status":  Equal(corev1.ConditionTrue),
			"Reason":  Equal(status.AvailableReason),
			"Message": Equal("DataScienceCluster resource reconciled successfully"),
		}))
	})

	// Case 2: When there is an error (err != nil)
	t.Run("Error sets ConditionFalse", func(t *testing.T) {
		err := errors.New("some error occurred")
		result := setAvailability(instance, err)

		g.Expect(result).To(BeFalse())
		g.Expect(instance.Status.Conditions).To(HaveLen(1))

		g.Expect(instance.Status.Conditions[0]).To(MatchFields(IgnoreExtras, Fields{
			"Type":    Equal(conditionsv1.ConditionAvailable),
			"Status":  Equal(corev1.ConditionFalse),
			"Reason":  Equal(status.DegradedReason),
			"Message": Equal(fmt.Sprintf("DataScienceCluster resource reconciled with errors: %v", err)),
		}))
	})
}

func createEnvTest(s *runtime.Scheme) (*envtest.Environment, error) {
	utilruntime.Must(corev1.AddToScheme(s))
	utilruntime.Must(appsv1.AddToScheme(s))
	utilruntime.Must(apiextensionsv1.AddToScheme(s))
	utilruntime.Must(componentApi.AddToScheme(s))
	utilruntime.Must(dscv1.AddToScheme(s))
	utilruntime.Must(dsciv1.AddToScheme(s))

	projectDir, err := envtestutil.FindProjectRoot()
	if err != nil {
		return nil, err
	}

	envTest := envtest.Environment{
		CRDInstallOptions: envtest.CRDInstallOptions{
			Scheme: s,
			Paths: []string{
				filepath.Join(projectDir, "config", "crd", "bases"),
			},
			ErrorIfPathMissing: true,
			CleanUpAfterUse:    false,
		},
	}

	return &envTest, nil
}

type MockComponentHandler struct {
	mock.Mock
}

func (m *MockComponentHandler) Init(platform cluster.Platform) error {
	args := m.Called(platform)
	return args.Error(0)
}

func (m *MockComponentHandler) GetName() string {
	args := m.Called()
	return args.String(0)
}

func (m *MockComponentHandler) GetManagementState(instance *dscv1.DataScienceCluster) operatorv1.ManagementState {
	args := m.Called(instance)

	//nolint:errcheck,forcetypeassert
	return args.Get(0).(operatorv1.ManagementState)
}

func (m *MockComponentHandler) NewCRObject(instance *dscv1.DataScienceCluster) common.PlatformObject {
	args := m.Called(instance)

	//nolint:errcheck,forcetypeassert
	return args.Get(0).(common.PlatformObject)
}

func (m *MockComponentHandler) NewComponentReconciler(ctx context.Context, mgr controllerruntime.Manager) error {
	args := m.Called(ctx, mgr)
	return args.Error(0)
}

func (m *MockComponentHandler) UpdateDSCStatus(dsc *dscv1.DataScienceCluster, obj client.Object) error {
	args := m.Called(dsc, obj)
	return args.Error(0)
}

func TestReconcileComponent(t *testing.T) {
	ctx := context.Background()

	g := NewWithT(t)
	s := runtime.NewScheme()

	envTest, err := createEnvTest(s)
	g.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	cfg, err := envTest.Start()
	g.Expect(err).NotTo(HaveOccurred())

	envTestClient, err := client.New(cfg, client.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	cli, err := odhClient.NewFromConfig(cfg, envTestClient)
	g.Expect(err).NotTo(HaveOccurred())

	// Create a DataScienceCluster instance
	instance := &dscv1.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
	}

	err = cli.Create(ctx, instance)
	g.Expect(err).ShouldNot(HaveOccurred())

	component := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.DashboardInstanceName,
		},
	}

	err = resources.EnsureGroupVersionKind(cli.Scheme(), instance)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = resources.EnsureGroupVersionKind(cli.Scheme(), component)
	g.Expect(err).ShouldNot(HaveOccurred())

	t.Run(string(operatorv1.Managed), func(t *testing.T) {
		g := NewWithT(t)

		mockHandler := new(MockComponentHandler)
		mockHandler.On("GetManagementState", mock.Anything).Return(operatorv1.Managed)
		mockHandler.On("NewCRObject", mock.Anything).Return(component.DeepCopy())

		err = reconcileComponent(ctx, cli, instance, mockHandler)
		g.Expect(err).ShouldNot(HaveOccurred())

		mockHandler.AssertExpectations(t)

		err = cli.Get(ctx, client.ObjectKeyFromObject(component), component)
		g.Expect(err).ShouldNot(HaveOccurred())
		g.Expect(component).Should(And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
		))
	})

	t.Run(string(operatorv1.Removed), func(t *testing.T) {
		g := NewWithT(t)

		mockHandler := new(MockComponentHandler)
		mockHandler.On("GetManagementState", mock.Anything).Return(operatorv1.Removed)
		mockHandler.On("NewCRObject", mock.Anything).Return(component.DeepCopy())

		err = reconcileComponent(ctx, cli, instance, mockHandler)
		g.Expect(err).ShouldNot(HaveOccurred())

		mockHandler.AssertExpectations(t)

		g.Expect(component).Should(
			// when using testenv, there are no controller so background propagation policy
			// does not work, hence to check if the object as been marked to be deleted, we
			// can rely on the deletionTimestamp
			jq.Match(`.metadata.deletionTimestamp != 0`),
		)
	})

	t.Run(string(operatorv1.Unmanaged), func(t *testing.T) {
		g := NewWithT(t)

		mockHandler := new(MockComponentHandler)
		mockHandler.On("GetManagementState", mock.Anything).Return(operatorv1.Unmanaged)
		mockHandler.On("NewCRObject", mock.Anything).Return(component.DeepCopy())

		err = reconcileComponent(ctx, cli, instance, mockHandler)
		g.Expect(err).Should(
			MatchError(ContainSubstring("unsupported management state: " + string(operatorv1.Unmanaged))),
		)

		mockHandler.AssertExpectations(t)
	})
}

func TestReconcileComponents(t *testing.T) {
	ctx := context.Background()

	g := NewWithT(t)
	s := runtime.NewScheme()

	envTest, err := createEnvTest(s)
	g.Expect(err).NotTo(HaveOccurred())

	t.Cleanup(func() {
		_ = envTest.Stop()
	})

	cfg, err := envTest.Start()
	g.Expect(err).NotTo(HaveOccurred())

	envTestClient, err := client.New(cfg, client.Options{Scheme: s})
	g.Expect(err).NotTo(HaveOccurred())

	cli, err := odhClient.NewFromConfig(cfg, envTestClient)
	g.Expect(err).NotTo(HaveOccurred())

	// Create a DataScienceCluster instance
	instance := &dscv1.DataScienceCluster{
		ObjectMeta: metav1.ObjectMeta{
			Name: "test-cluster",
		},
	}

	err = cli.Create(ctx, instance)
	g.Expect(err).ShouldNot(HaveOccurred())

	component := &componentApi.Dashboard{
		ObjectMeta: metav1.ObjectMeta{
			Name: componentApi.DashboardInstanceName,
		},
	}

	err = resources.EnsureGroupVersionKind(cli.Scheme(), instance)
	g.Expect(err).ShouldNot(HaveOccurred())

	err = resources.EnsureGroupVersionKind(cli.Scheme(), component)
	g.Expect(err).ShouldNot(HaveOccurred())

	t.Run("reconcileComponent succeed", func(t *testing.T) {
		g := NewWithT(t)

		t.Cleanup(func() {
			err := cli.Delete(ctx, component)
			g.Expect(client.IgnoreNotFound(err)).ShouldNot(HaveOccurred())
		})

		mockHandler := new(MockComponentHandler)
		mockHandler.On("GetManagementState", mock.Anything).Return(operatorv1.Managed)
		mockHandler.On("NewCRObject", mock.Anything).Return(component.DeepCopy())
		mockHandler.On("UpdateDSCStatus", mock.Anything, mock.Anything).Return(nil)

		r := cr.Registry{}
		r.Add(mockHandler)

		c := component.DeepCopy()

		err := cli.Create(ctx, c)
		g.Expect(err).ShouldNot(HaveOccurred())

		meta.SetStatusCondition(&c.Status.Conditions, metav1.Condition{
			Type:    status.ConditionTypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  status.ReadyReason,
			Message: status.ReadyReason,
		})

		err = cli.Status().Update(ctx, c)
		g.Expect(err).ShouldNot(HaveOccurred())

		reconcileComponents(ctx, cli, instance, &r)

		mockHandler.AssertExpectations(t)
		g.Expect(instance).Should(And(
			WithTransform(
				jq.Extract(`.status.conditions[] | select(.type == "%s")`, conditionsv1.ConditionAvailable), And(
					jq.Match(`.status == "%s"`, metav1.ConditionTrue),
					jq.Match(`.reason == "%s"`, status.AvailableReason),
					jq.Match(`.message | contains("reconciled successfully")`),
				)),
			WithTransform(
				jq.Extract(`.status.conditions[] | select(.type == "%s")`, status.ConditionTypeReady), And(
					jq.Match(`.status == "%s"`, metav1.ConditionTrue),
					jq.Match(`.reason == "%s"`, status.ReadyReason),
					jq.Match(`.message == "%s"`, status.ReadyReason),
				)),
		))
	})

	t.Run("reconcileComponent component not ready", func(t *testing.T) {
		g := NewWithT(t)

		t.Cleanup(func() {
			err := cli.Delete(ctx, component)
			g.Expect(client.IgnoreNotFound(err)).ShouldNot(HaveOccurred())
		})

		mockHandler := new(MockComponentHandler)
		mockHandler.On("GetName").Return(componentApi.DashboardComponentName)
		mockHandler.On("GetManagementState", mock.Anything).Return(operatorv1.Managed)
		mockHandler.On("NewCRObject", mock.Anything).Return(component.DeepCopy())
		mockHandler.On("UpdateDSCStatus", mock.Anything, mock.Anything).Return(nil)

		r := cr.Registry{}
		r.Add(mockHandler)

		c := component.DeepCopy()

		err := cli.Create(ctx, c)
		g.Expect(err).ShouldNot(HaveOccurred())

		meta.SetStatusCondition(&c.Status.Conditions, metav1.Condition{
			Type:    status.ConditionTypeReady,
			Status:  metav1.ConditionFalse,
			Reason:  status.ReadyReason,
			Message: status.ReadyReason,
		})

		err = cli.Status().Update(ctx, c)
		g.Expect(err).ShouldNot(HaveOccurred())

		reconcileComponents(ctx, cli, instance, &r)

		mockHandler.AssertExpectations(t)
		g.Expect(instance).Should(And(
			WithTransform(
				jq.Extract(`.status.conditions[] | select(.type == "%s")`, conditionsv1.ConditionAvailable), And(
					jq.Match(`.status == "%s"`, metav1.ConditionTrue),
					jq.Match(`.reason == "%s"`, status.AvailableReason),
					jq.Match(`.message | contains("reconciled successfully")`),
				)),
			WithTransform(
				jq.Extract(`.status.conditions[] | select(.type == "%s")`, status.ConditionTypeReady), And(
					jq.Match(`.status == "%s"`, metav1.ConditionFalse),
					jq.Match(`.reason == "%s"`, status.NotReadyReason),
					jq.Match(`.message | contains("dashboard")`),
				)),
		))
	})

	t.Run("reconcileComponent reconcile failure", func(t *testing.T) {
		g := NewWithT(t)

		t.Cleanup(func() {
			err := cli.Delete(ctx, component)
			g.Expect(client.IgnoreNotFound(err)).ShouldNot(HaveOccurred())
		})

		mockHandler := new(MockComponentHandler)
		mockHandler.On("GetManagementState", mock.Anything).Return(operatorv1.Unmanaged)
		mockHandler.On("NewCRObject", mock.Anything).Return(component.DeepCopy())
		mockHandler.On("UpdateDSCStatus", mock.Anything, mock.Anything).Return(nil)

		r := cr.Registry{}
		r.Add(mockHandler)

		reconcileComponents(ctx, cli, instance, &r)

		mockHandler.AssertExpectations(t)
		g.Expect(instance).Should(And(
			WithTransform(
				jq.Extract(`.status.conditions[] | select(.type == "%s")`, conditionsv1.ConditionAvailable), And(
					jq.Match(`.status == "%s"`, metav1.ConditionFalse),
					jq.Match(`.reason == "%s"`, status.DegradedReason),
					jq.Match(`.message | contains("unsupported management state")`),
				)),
			WithTransform(
				jq.Extract(`.status.conditions[] | select(.type == "%s")`, status.ConditionTypeReady), And(
					jq.Match(`.status == "%s"`, metav1.ConditionTrue),
					jq.Match(`.reason == "%s"`, status.ReadyReason),
					jq.Match(`.message == "%s"`, status.ReadyReason),
				)),
		))
	})

	t.Run("reconcileComponent update status failure", func(t *testing.T) {
		g := NewWithT(t)

		t.Cleanup(func() {
			err := cli.Delete(ctx, component)
			g.Expect(client.IgnoreNotFound(err)).ShouldNot(HaveOccurred())
		})

		mockHandler := new(MockComponentHandler)
		mockHandler.On("GetManagementState", mock.Anything).Return(operatorv1.Managed)
		mockHandler.On("NewCRObject", mock.Anything).Return(component.DeepCopy())
		mockHandler.On("UpdateDSCStatus", mock.Anything, mock.Anything).Return(errors.New("failure"))

		r := cr.Registry{}
		r.Add(mockHandler)

		c := component.DeepCopy()

		err := cli.Create(ctx, c)
		g.Expect(err).ShouldNot(HaveOccurred())

		meta.SetStatusCondition(&c.Status.Conditions, metav1.Condition{
			Type:    status.ConditionTypeReady,
			Status:  metav1.ConditionTrue,
			Reason:  status.ReadyReason,
			Message: status.ReadyReason,
		})

		err = cli.Status().Update(ctx, c)
		g.Expect(err).ShouldNot(HaveOccurred())

		reconcileComponents(ctx, cli, instance, &r)

		mockHandler.AssertExpectations(t)
		g.Expect(instance).Should(And(
			WithTransform(
				jq.Extract(`.status.conditions[] | select(.type == "%s")`, conditionsv1.ConditionAvailable), And(
					jq.Match(`.status == "%s"`, metav1.ConditionFalse),
					jq.Match(`.reason == "%s"`, status.DegradedReason),
					jq.Match(`.message | contains("failure")`),
				)),
			WithTransform(
				jq.Extract(`.status.conditions[] | select(.type == "%s")`, status.ConditionTypeReady), And(
					jq.Match(`.status == "%s"`, metav1.ConditionTrue),
					jq.Match(`.reason == "%s"`, status.ReadyReason),
					jq.Match(`.message == "%s"`, status.ReadyReason),
				)),
		))
	})
}
