package ossm

import (
	"context"
	"fmt"
	multierror "github.com/hashicorp/go-multierror"
	kfapisv3 "github.com/opendatahub-io/opendatahub-operator/apis"
	kftypesv3 "github.com/opendatahub-io/opendatahub-operator/apis/apps"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfconfig"
	"github.com/opendatahub-io/opendatahub-operator/pkg/kfconfig/ossmplugin"
	"github.com/pkg/errors"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	clientset "k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"regexp"
	ctrlLog "sigs.k8s.io/controller-runtime/pkg/log"
	"strings"
)

const (
	PluginName = "KfOssmPlugin"
)

var log = ctrlLog.Log.WithName(PluginName)

type Ossm struct {
	*kfconfig.KfConfig
	pluginSpec *ossmplugin.OssmPluginSpec
	config     *rest.Config
	manifests  []manifest
}

func NewOssm(kfConfig *kfconfig.KfConfig, restConfig *rest.Config) *Ossm {
	return &Ossm{
		KfConfig: kfConfig,
		config:   restConfig,
	}

}

// GetPlatform returns the ossm kfapp. It's called by coordinator.GetPlatform
func GetPlatform(kfConfig *kfconfig.KfConfig) (kftypesv3.Platform, error) {
	return NewOssm(kfConfig, kftypesv3.GetConfig()), nil
}

// GetPluginSpec gets the plugin spec.
func (ossm *Ossm) GetPluginSpec() (*ossmplugin.OssmPluginSpec, error) {
	if ossm.pluginSpec != nil {
		return ossm.pluginSpec, nil
	}

	ossm.pluginSpec = &ossmplugin.OssmPluginSpec{}
	err := ossm.KfConfig.GetPluginSpec(PluginName, ossm.pluginSpec)

	return ossm.pluginSpec, err
}

func (ossm *Ossm) Init(_ kftypesv3.ResourceEnum) error {
	if ossm.KfConfig.Spec.SkipInitProject {
		log.Info("Skipping init phase")
	}

	log.Info("Initializing " + PluginName)
	pluginSpec, err := ossm.GetPluginSpec()
	if err != nil {
		return internalError(errors.WithStack(err))
	}

	pluginSpec.SetDefaults()

	if valid, reason := pluginSpec.IsValid(); !valid {
		return internalError(errors.New(reason))
	}

	// TODO ensure operators are installed

	if err := ossm.createConfigMap("service-mesh-refs",
		map[string]string{
			"CONTROL_PLANE_NAME": pluginSpec.Mesh.Name,
			"MESH_NAMESPACE":     pluginSpec.Mesh.Namespace,
		}); err != nil {
		return internalError(err)
	}

	if err := ossm.createConfigMap("auth-refs",
		map[string]string{
			"AUTHORINO_LABEL": pluginSpec.Auth.Authorino.Label,
		}); err != nil {
		return internalError(err)
	}

	if err := ossm.MigrateDSProjects(); err != nil {
		log.Error(err, "failed migrating Data Science Projects")
	}

	if err := ossm.processManifests(); err != nil {
		return internalError(err)
	}

	return nil
}

func (ossm *Ossm) Generate(resources kftypesv3.ResourceEnum) error {
	// TODO sort by Kind as .Apply does
	if err := ossm.applyManifests(); err != nil {
		return internalError(errors.WithStack(err))
	}

	return nil
}

// ExtractHostName strips given URL in string from http(s):// prefix and subsequent path.
// This is useful when getting value from http headers (such as origin).
// If given string does not start with http(s) prefix it will be returned as is.
func ExtractHostName(s string) string {
	r := regexp.MustCompile(`^(https?://)`)
	withoutProtocol := r.ReplaceAllString(s, "")
	if s == withoutProtocol {
		return s
	}
	index := strings.Index(withoutProtocol, "/")
	if index == -1 {
		return withoutProtocol
	}
	return withoutProtocol[:index]
}

func (ossm *Ossm) createConfigMap(cfgMapName string, data map[string]string) error {

	configMap := &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      cfgMapName,
			Namespace: ossm.KfConfig.Namespace,
		},
		Data: data,
	}

	client, err := clientset.NewForConfig(ossm.config)
	if err != nil {
		return err
	}

	configMaps := client.CoreV1().ConfigMaps(configMap.Namespace)
	_, err = configMaps.Get(context.TODO(), configMap.Name, metav1.GetOptions{})
	if k8serrors.IsNotFound(err) {
		_, err = configMaps.Create(context.TODO(), configMap, metav1.CreateOptions{})
		if err != nil {
			return err
		}

	} else if k8serrors.IsAlreadyExists(err) {
		_, err = configMaps.Update(context.TODO(), configMap, metav1.UpdateOptions{})
		if err != nil {
			return err
		}
	} else {
		return err
	}

	return nil
}

func (ossm *Ossm) MigrateDSProjects() error {

	client, err := clientset.NewForConfig(ossm.config)
	if err != nil {
		return err
	}

	selector := labels.SelectorFromSet(labels.Set{"opendatahub.io/dashboard": "true"})

	namespaces, err := client.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{LabelSelector: selector.String()})
	if err != nil {
		return fmt.Errorf("failed to get namespaces: %v", err)
	}

	var result *multierror.Error

	for _, ns := range namespaces.Items {
		annotations := ns.GetAnnotations()
		if annotations == nil {
			annotations = map[string]string{}
		}
		annotations["opendatahub.io/service-mesh"] = "true"
		ns.SetAnnotations(annotations)

		if _, err := client.CoreV1().Namespaces().Update(context.TODO(), &ns, metav1.UpdateOptions{}); err != nil {
			result = multierror.Append(result, err)
		}
	}

	return result.ErrorOrNil()
}

// TODO handle delete

func internalError(err error) error {
	return &kfapisv3.KfError{
		Code:    int(kfapisv3.INTERNAL_ERROR),
		Message: fmt.Sprintf("%+v", err),
	}
}
