package ossmplugin

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"reflect"
)

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// +k8s:openapi-gen=true
// Placeholder for the plugin API.
type KfOssmPlugin struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec OssmPluginSpec `json:"spec,omitempty"`
}

// OssmPluginSpec defines the extra data provided by the Openshift Service Mesh Plugin in KfDef spec.
type OssmPluginSpec struct {
	Mesh MeshSpec `json:"mesh,omitempty"`
	Auth AuthSpec `json:"auth,omitempty"`
}

type MeshSpec struct {
	Name        string   `json:"name,omitempty" default:"basic"`
	Namespace   string   `json:"namespace,omitempty" default:"istio-system"`
	Certificate CertSpec `json:"certificate,omitempty"`
}

type CertSpec struct {
	Name     string `json:"name,omitempty" default:"opendatahub-dashboard-cert"`
	Generate bool   `json:"generate,omitempty"`
}

type AuthSpec struct {
	Name      string        `json:"name,omitempty" default:"authorino"`
	Namespace string        `json:"namespace,omitempty" default:"auth-provider"`
	Authorino AuthorinoSpec `json:"authorino,omitempty"`
}

type AuthorinoSpec struct {
	Name  string `json:"name,omitempty" default:"authorino-mesh-authz-provider"`
	Label string `json:"label,omitempty" default:"authorino/topic=odh"`
	Image string `json:"image,omitempty" default:"quay.io/kuadrant/authorino:v0.13.0"`
}

// IsValid returns true if the spec is a valid and complete spec.
// If false it will also return a string providing a message about why its invalid.
func (plugin *OssmPluginSpec) IsValid() (bool, string) {

	if plugin.Auth.Name != "authorino" {
		return false, "only authorino is available as authorization layer"
	}

	return true, ""
}

func (plugin *OssmPluginSpec) SetDefaults() {
	setDefaults(plugin)
}

func setDefaults(obj interface{}) {
	value := reflect.ValueOf(obj).Elem()
	typ := value.Type()

	for i := 0; i < typ.NumField(); i++ {
		field := value.Field(i)
		tag := typ.Field(i).Tag.Get("default")

		if tag != "" && field.CanSet() && isEmptyValue(field) {
			defaultValue := reflect.ValueOf(tag)
			field.Set(defaultValue)
		}

		if field.Kind() == reflect.Struct {
			setDefaults(field.Addr().Interface())
		}
	}
}

func isEmptyValue(value reflect.Value) bool {
	zero := reflect.Zero(value.Type()).Interface()
	return reflect.DeepEqual(value.Interface(), zero)
}
