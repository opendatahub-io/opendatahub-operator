package components

import (
	"context"
	"fmt"

	dsci "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	operatorv1 "github.com/openshift/api/operator/v1"
	errors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"time"
)

type Component struct {
	// Set to one of the following values:
	//
	// - "Managed" : the operator is actively managing the component and trying to keep it active.
	//               It will only upgrade the component if it is safe to do so
	//
	// - "Removed" : the operator is actively managing the component and will not install it,
	//               or if it is installed, the operator will try to remove it
	//
	// +kubebuilder:validation:Enum=Managed;Removed
	ManagementState operatorv1.ManagementState `json:"managementState,omitempty"`
	// Add any other common fields across components below

	// Add developer fields
	// +optional
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=2
	DevFlags DevFlags `json:"devFlags,omitempty"`
}

func (c *Component) GetManagementState() operatorv1.ManagementState {
	return c.ManagementState
}

func (c *Component) Cleanup(_ client.Client, _ *dsci.DSCInitializationSpec) error {
	// noop
	return nil
}

// DevFlags defines list of fields that can be used by developers to test customizations. This is not recommended
// to be used in production environment.
type DevFlags struct {
	// List of custom manifests for the given component
	// +optional
	Manifests []ManifestsConfig `json:"manifests,omitempty"`
}

type ManifestsConfig struct {
	// uri is the URI point to a git repo with tag/branch. e.g  https://github.com/org/repo/tarball/<tag/branch>
	// +optional
	// +kubebuilder:default:=""
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=1
	URI string `json:"uri,omitempty"`

	// contextDir is the relative path to the folder containing manifests in a repository
	// +optional
	// +kubebuilder:default:=""
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=2
	ContextDir string `json:"contextDir,omitempty"`

	// sourcePath is the subpath within contextDir where kustomize builds start. Examples include any sub-folder or path: `base`, `overlays/dev`, `default`, `odh` etc
	// +optional
	// +kubebuilder:default:=""
	// +operator-sdk:csv:customresourcedefinitions:type=spec,order=3
	SourcePath string `json:"sourcePath,omitempty"`
}

type ComponentInterface interface {
	ReconcileComponent(cli client.Client, owner metav1.Object, DSCISpec *dsci.DSCInitializationSpec) error
	Cleanup(cli client.Client, DSCISpec *dsci.DSCInitializationSpec) error
	GetComponentName() string
	GetManagementState() operatorv1.ManagementState
	SetImageParamsMap(imageMap map[string]string) map[string]string
	OverrideManifests(platform string) error
	WaitForDeploymentAvailable(ctx context.Context, r *rest.Config, c string, n string, i int, t int) error
}

func (c *Component) SetImageParamsMap(imageMap map[string]string) map[string]string {
	return imageMap
}

func (c *Component) WaitForDeploymentAvailable(ctx context.Context, restConfig *rest.Config, componentName string, namespace string, interval int, timeout int) error {
	resourceInterval := time.Duration(interval) * time.Second
	resourceTimeout := time.Duration(timeout) * time.Minute
	return wait.PollUntilContextTimeout(context.TODO(), resourceInterval, resourceTimeout, true, func(ctx context.Context) (bool, error) {
		clientset, err := kubernetes.NewForConfig(restConfig)
		if err != nil {
			return false, fmt.Errorf("error getting client %v", err)
		}
		componentDeploymentList, err := clientset.AppsV1().Deployments(namespace).List(context.TODO(), metav1.ListOptions{
			LabelSelector: "app.opendatahub.io/" + componentName,
		})
		if err != nil {
			if errors.IsNotFound(err) {
				return false, nil
			}
		}
		isReady := false
		if len(componentDeploymentList.Items) != 0 {
			for _, deployment := range componentDeploymentList.Items {
				if deployment.Status.ReadyReplicas == deployment.Status.Replicas {
					isReady = true
				} else {
					isReady = false
				}
			}
		}
		return isReady, nil
	})
}
