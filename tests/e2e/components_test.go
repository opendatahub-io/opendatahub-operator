package e2e_test

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/blang/semver/v4"
	configv1 "github.com/openshift/api/config/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/opendatahub-io/opendatahub-operator/v2/api/common"
	dscv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/datasciencecluster/v1"
	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/api/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/internal/controller/status"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/metadata/labels"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/resources"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/matchers/jq"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/utils/test/testf"

	. "github.com/onsi/gomega"
)

type ComponentTestCtx struct {
	*testf.TestContext

	GVK                  schema.GroupVersionKind
	DSCName              types.NamespacedName
	DSCIName             types.NamespacedName
	ApplicationNamespace string
}

func NewComponentTestCtx(object common.PlatformObject) (*ComponentTestCtx, error) {
	tcf, err := testf.NewTestContext(
		testf.WithTOptions(
			testf.WithEventuallyTimeout(generalWaitTimeout),
			testf.WithEventuallyPollingInterval(generalPollInterval),
		),
	)

	if err != nil {
		return nil, err
	}

	ogvk, err := resources.GetGroupVersionKindForObject(tcf.Scheme(), object)
	if err != nil {
		return nil, err
	}

	dsciList := dsciv1.DSCInitializationList{}
	if err := tcf.Client().List(tcf.Context(), &dsciList); err != nil {
		return nil, err
	}

	if len(dsciList.Items) != 1 {
		return nil, fmt.Errorf("failure looking up DSCInitialization, expected=1, found=%d", len(dsciList.Items))
	}

	dscList := dscv1.DataScienceClusterList{}
	if err := tcf.Client().List(tcf.Context(), &dscList); err != nil {
		return nil, err
	}

	if len(dscList.Items) != 1 {
		return nil, fmt.Errorf("failure looking up DataScienceCluster, expected=1, found=%d", len(dscList.Items))
	}

	componentCtx := ComponentTestCtx{
		TestContext:          tcf,
		GVK:                  ogvk,
		DSCName:              client.ObjectKeyFromObject(&dscList.Items[0]),
		DSCIName:             client.ObjectKeyFromObject(&dsciList.Items[0]),
		ApplicationNamespace: dsciList.Items[0].Spec.ApplicationsNamespace,
	}

	return &componentCtx, nil
}

func (c *ComponentTestCtx) ValidateComponentEnabled(t *testing.T) {
	g := c.NewWithT(t)

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(`.spec.components.%s.managementState = "%s"`, strings.ToLower(c.GVK.Kind), operatorv1.Managed),
	).Eventually().Should(
		Succeed(),
	)

	g.List(gvk.DataScienceCluster).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.spec.components.%s.managementState == "%s"`, strings.ToLower(c.GVK.Kind), operatorv1.Managed),
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, c.GVK.Kind, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeComponentsReady, metav1.ConditionTrue),
		)),
	))

	g.List(c.GVK).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, gvk.DataScienceCluster.Kind),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeReady, metav1.ConditionTrue),
			jq.Match(`.status.conditions[] | select(.type == "%s") | .status == "%s"`, status.ConditionTypeProvisioningSucceeded, metav1.ConditionTrue),
		)),
	))
}

func (c *ComponentTestCtx) ValidateOperandsOwnerReferences(t *testing.T) {
	g := c.NewWithT(t)

	g.List(
		gvk.Deployment,
		client.InNamespace(c.ApplicationNamespace),
		client.MatchingLabels{labels.PlatformPartOf: strings.ToLower(c.GVK.Kind)},
	).Eventually().Should(And(
		Not(BeEmpty()),
		HaveEach(
			jq.Match(`.metadata.ownerReferences[0].kind == "%s"`, c.GVK.Kind),
		),
	))
}

func (c *ComponentTestCtx) ValidateUpdateDeploymentsResources(t *testing.T) {
	g := c.NewWithT(t)

	deployments := g.List(
		gvk.Deployment,
		client.InNamespace(c.ApplicationNamespace),
		client.MatchingLabels{
			labels.PlatformPartOf: strings.ToLower(c.GVK.Kind),
		},
	).Eventually().ShouldNot(
		BeEmpty(),
	)

	for _, d := range deployments {
		t.Run("deployment_"+d.GetName(), func(t *testing.T) {
			replicas, err := jq.ExtractValue[int](d, `.spec.replicas`)
			g.Expect(err).ShouldNot(HaveOccurred())

			expectedReplica := replicas + 1
			if replicas > 1 {
				expectedReplica = 1
			}

			g.Update(
				gvk.Deployment,
				client.ObjectKeyFromObject(&d),
				testf.Transform(`.spec.replicas = %d`, expectedReplica),
			).Eventually().WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(
				jq.Match(`.spec.replicas == %d`, expectedReplica),
			)

			g.Get(
				gvk.Deployment,
				client.ObjectKeyFromObject(&d),
			).Eventually().Should(
				jq.Match(`.spec.replicas == %d`, expectedReplica),
			)

			g.Get(
				gvk.Deployment,
				client.ObjectKeyFromObject(&d),
			).Consistently().WithTimeout(30 * time.Second).WithPolling(1 * time.Second).Should(
				jq.Match(`.spec.replicas == %d`, expectedReplica),
			)
		})
	}
}

func (c *ComponentTestCtx) ValidateComponentDisabled(t *testing.T) {
	g := c.NewWithT(t)

	g.List(c.GVK).Eventually().ShouldNot(
		BeEmpty(),
	)

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(`.spec.components.%s.managementState = "%s"`, strings.ToLower(c.GVK.Kind), operatorv1.Removed),
	).Eventually().Should(
		Succeed(),
	)

	g.List(c.GVK).Eventually().Should(
		BeEmpty(),
	)

	g.List(
		gvk.Deployment,
		client.InNamespace(c.ApplicationNamespace),
		client.MatchingLabels{
			labels.PlatformPartOf: strings.ToLower(c.GVK.Kind),
		},
	).Eventually().Should(
		BeEmpty(),
	)

	g.List(gvk.DataScienceCluster).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			jq.Match(`.spec.components.%s.managementState == "%s"`, strings.ToLower(c.GVK.Kind), operatorv1.Removed),
			jq.Match(`.status.conditions[] | select(.type == "%sReady") | .status == "%s"`, c.GVK.Kind, metav1.ConditionFalse),
		)),
	))
}

func (c *ComponentTestCtx) ValidateCRDReinstated(t *testing.T, name string, version ...string) {
	t.Helper()

	g := c.NewWithT(t)
	crdSel := client.MatchingFields{"metadata.name": name}

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(`.spec.components.%s.managementState = "%s"`, strings.ToLower(c.GVK.Kind), operatorv1.Removed),
	).Eventually().Should(
		Succeed(),
	)

	g.List(c.GVK).Eventually().Should(
		BeEmpty(),
	)
	g.List(gvk.CustomResourceDefinition, crdSel).Eventually().Should(
		HaveLen(1),
	)

	g.Delete(
		gvk.CustomResourceDefinition,
		types.NamespacedName{Name: name},
		client.PropagationPolicy(metav1.DeletePropagationForeground),
	).Eventually().Should(
		Succeed(),
	)

	g.List(gvk.CustomResourceDefinition, crdSel).Eventually().Should(
		BeEmpty(),
	)

	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(`.spec.components.%s.managementState = "%s"`, strings.ToLower(c.GVK.Kind), operatorv1.Managed),
	).Eventually().Should(
		Succeed(),
	)

	g.List(c.GVK).Eventually().Should(
		HaveLen(1),
	)
	g.List(gvk.CustomResourceDefinition, crdSel).Eventually().Should(
		HaveLen(1),
	)
	if len(version) != 0 {
		g.Get(
			gvk.CustomResourceDefinition,
			types.NamespacedName{Name: name},
		).Eventually(5*time.Second, 500*time.Millisecond).Should(
			jq.Match(`.status.storedVersions[0] == "%s"`, version[0]),
		)
	}
}

// Validate releases for any component in the DataScienceCluster.
func (c *ComponentTestCtx) ValidateComponentReleases(t *testing.T) {
	t.Helper()

	g := c.NewWithT(t)

	componentName := strings.ToLower(c.GVK.Kind)

	// Transform the DataScienceCluster to set the management state of the component
	g.Update(
		gvk.DataScienceCluster,
		c.DSCName,
		testf.Transform(
			`.spec.components.%s.managementState = "%s"`, componentName, operatorv1.Managed,
		),
	).Eventually().Should(
		jq.Match(`.spec.components.%s.managementState == "%s"`, componentName, operatorv1.Managed),
	)

	// Check if the releases field contains multiple releases for the component
	g.List(gvk.DataScienceCluster).Eventually().Should(And(
		HaveLen(1),
		HaveEach(
			// Check releases for the component itself
			jq.Match(`.status.components.%s.releases | length > 0`, componentName),
		),
	))

	// Validate each release's fields (name, version, repoUrl) using HaveEach
	g.List(gvk.DataScienceCluster).Eventually().Should(And(
		HaveLen(1),
		HaveEach(And(
			// Check that each release has the required fields (name, version, repoUrl)
			jq.Match(`.status.components.%s.releases[].name != ""`, componentName),
			jq.Match(`.status.components.%s.releases[].version != ""`, componentName),
			jq.Match(`.status.components.%s.releases[].repoUrl != ""`, componentName)),
		),
	))
}

func (c *ComponentTestCtx) GetDSC() (*dscv1.DataScienceCluster, error) {
	obj := dscv1.DataScienceCluster{}

	err := c.Client().Get(c.Context(), c.DSCName, &obj)
	if err != nil {
		return nil, err
	}

	return &obj, nil
}

func (c *ComponentTestCtx) GetDSCI() (*dsciv1.DSCInitialization, error) {
	obj := dsciv1.DSCInitialization{}

	err := c.Client().Get(c.Context(), c.DSCIName, &obj)
	if err != nil {
		return nil, err
	}

	return &obj, nil
}

func (c *ComponentTestCtx) GetClusterVersion() (semver.Version, error) {
	clusterVersion := &configv1.ClusterVersion{}
	if err := c.Client().Get(c.Context(), client.ObjectKey{
		Name: cluster.OpenShiftVersionObj,
	}, clusterVersion); err != nil {
		return semver.Version{}, err
	}
	return semver.ParseTolerant(clusterVersion.Status.History[0].Version)
}
