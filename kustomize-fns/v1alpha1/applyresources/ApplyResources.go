package main

import (
	"fmt"
	log "github.com/sirupsen/logrus"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	kubeTypes "k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/discovery/cached/memory"
	"k8s.io/client-go/dynamic"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"sigs.k8s.io/kustomize/v3/pkg/ifc"
	"sigs.k8s.io/kustomize/v3/pkg/resmap"
	"sigs.k8s.io/kustomize/v3/pkg/resource"
	"sigs.k8s.io/kustomize/v3/pkg/transformers/config"
	"sigs.k8s.io/kustomize/v3/pkg/types"
	"sigs.k8s.io/yaml"
)

const (
	// configurableResourcesLabel is a label added by odh dev to specify which objects are can be updated.
	configurableResourcesLabel = "opendatahub.io/configurable"
	// modifiedResourcesLabel is a label added by end user to specify a given resource has been updated.
	modifiedResourcesLabel     = "opendatahub.io/modified"
	// needsUpdateResourcesLabel is a label added by odh devs to specify that an object needs to be updated with latest
	// changes irrespective of the modified label.
	needsUpdateResourcesLabel  = "opendatahub.io/needs-update"
)

type Spec struct {
	FieldSpecs []config.FieldSpec `yaml:"fieldSpecs"`
}

type plugin struct {
	rmf *resmap.Factory
	ldr ifc.Loader
	c   *resmap.Configurable

	types.ObjectMeta `json:"metadata,omitempty" yaml:"metadata,omitempty" protobuf:"bytes,1,opt,name=metadata"`
	Spec             Spec `yaml:"spec"`
}

//nolint: golint
//noinspection GoUnusedGlobalVariable
var KustomizePlugin plugin


func (p *plugin) Config(ldr ifc.Loader, rf *resmap.Factory, c []byte) error {
	p.ldr = ldr
	p.rmf = rf
	return yaml.Unmarshal(c, p)
}

func (p *plugin) Transform(m resmap.ResMap) error {
	log.Info("Inside the transform function")
	inClusterconfig, err := rest.InClusterConfig()
	if err != nil {
		return fmt.Errorf("error getting incluster config %v", err)
	}
	dc, err := discovery.NewDiscoveryClientForConfig(inClusterconfig)
	if err != nil {
		return fmt.Errorf("error getting discovery client config %v", err)

	}
	mapper := restmapper.NewDeferredDiscoveryRESTMapper(memory.NewMemCacheClient(dc))

	// 2. Prepare the dynamic client
	dyn, err := dynamic.NewForConfig(inClusterconfig)
	if err != nil {

		return fmt.Errorf("error getting dynamic config %v", err)
	}

	for _, fs := range p.Spec.FieldSpecs {
		for _, r := range m.Resources() {
			if r.OrgId().IsSelected(&fs.Gvk) {
				localObjectLabels := r.GetLabels()

				mapping, err := mapper.RESTMapping(schema.GroupKind{
					Group: r.GetGvk().Group,
					Kind:  r.GetGvk().Kind}, r.GetGvk().Version)
				if err != nil {

					return fmt.Errorf("error mapping rest config %v", err)
				}
				res, err := dyn.Resource(mapping.Resource).Namespace(r.GetNamespace()).Get(r.GetName(), metav1.GetOptions{})
				if err != nil {
					log.Printf("Error getting resources %v: %v ", r.GetName(), err)
					continue
				}
				clusterObjectLabels := res.GetLabels()

				if configLabelval, ok := localObjectLabels[configurableResourcesLabel]; ok {
					if configLabelval == "true" {
						if modLabelVal, ok := clusterObjectLabels[modifiedResourcesLabel]; ok {
							if modLabelVal == "true" {
								needsUpdateLabelVal, ok := localObjectLabels[needsUpdateResourcesLabel]
								if ok && needsUpdateLabelVal == "true" {
									// Patch resources with new values during upgrade.
									err := getResourcesPatched(res, r)
									if err != nil {
										return fmt.Errorf("error patching resource %v : %v", r.GetName(), err)
									}
								}
									err := m.Remove(r.CurId())
									if err != nil {
										return fmt.Errorf("error removing resource from the map: %v ", err)
									}else {
										log.Printf("Resource is %v removed from resource map", r.GetName())
									}
								}

							}
						}
					}
				}
			}

		}
	return nil
}

// getResourcesPatched adds new values/fields to the resource types Secrets and ConfigMaps
func getResourcesPatched(clusterResource *unstructured.Unstructured, localResource *resource.Resource) error {
	con, err := rest.InClusterConfig()
	if err != nil{
		return err
	}
	corev1client, err := corev1.NewForConfig(con)
	if err != nil{
		return err
	}

	resourceKind := clusterResource.GetKind()
	clusterData := clusterResource.UnstructuredContent()["data"]
	localData := localResource.Map()["data"]

		for key, val := range localData.(map[string]interface{}){
			if _, ok := clusterData.(map[string]interface{})[key]; !ok {
				patchData := fmt.Sprintf("[{\"op\":\"add\",\"path\":\"/data/%s\",\"value\": \"%s\"}]", key, val)
				if resourceKind == "Secret" {
					_, err := corev1client.Secrets(clusterResource.GetNamespace()).Patch(clusterResource.GetName(), kubeTypes.JSONPatchType, []byte(patchData))
					if err != nil {
						return err
					}
				} else if resourceKind == "ConfigMap"{
					_, err := corev1client.ConfigMaps(clusterResource.GetNamespace()).Patch(clusterResource.GetName(), kubeTypes.JSONPatchType, []byte(patchData))
					if err != nil {
						return err
					}
				}
			}
		}

	return nil

}

