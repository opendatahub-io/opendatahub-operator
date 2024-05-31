package observability

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	ttemplate "text/template"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

// UpdatePrometheusConfigNew creates or remove a PrometheusRule CR for the component.
func UpdatePrometheusConfigNew(ctx context.Context, cli client.Client, enable bool, component string, rootFS embed.FS, dscispec *dsciv1.DSCInitializationSpec) error {
	// feature.CreateTemplateManifestFrom()
	var object *unstructured.Unstructured

	promRule := &unstructured.Unstructured{}
	promRule.SetGroupVersionKind(gvk.PrometheusRule)
	err := cli.Get(ctx, client.ObjectKey{Name: component + "-prometheusrules", Namespace: dscispec.Monitoring.Namespace}, promRule)
	if !apierrs.IsNotFound(err) {
		return fmt.Errorf("failed fetching PrometheusRules CR: %w", err)
	}
	if enable && apierrs.IsNotFound(err) { // we should create if not exist
		object, err := UpdatePromTemplate(component, rootFS, dscispec)
		if err != nil {
			return fmt.Errorf("failed inject template for PrometheusRules CR: %w", err)
		}
		err = cli.Create(ctx, object)
		if err != nil {
			return fmt.Errorf("error creating PrometheusRules on component %s: %w", component, err)
		}
	}
	if !enable && err != nil { // we should remove
		err = cli.Delete(ctx, object)
		if err != nil {
			return fmt.Errorf("error removing PrometheusRules on component %s: %w", component, err)
		}
	}
	return nil
}

func UpdatePromTemplate(component string, rootFS embed.FS, dscispec *dsciv1.DSCInitializationSpec) (*unstructured.Unstructured, error) {
	promTemplate, err := rootFS.ReadFile(component + "-rules.temp.yaml")
	if err != nil {
		return nil, fmt.Errorf("failed to read prometheus rule file: %w", err)
	}
	tmpl, err := ttemplate.New("promrules").Delims("[[", "]]").Parse(string(promTemplate))

	if err != nil {
		return nil, fmt.Errorf("failed to parse template: %w", err)
	}
	var buffer bytes.Buffer
	if err = tmpl.Execute(&buffer, dscispec); err != nil {
		return nil, fmt.Errorf("failed to execute template: %w", err)
	}
	resources := buffer.String()

	obj := &unstructured.Unstructured{}
	if err = yaml.Unmarshal([]byte(resources), obj); err != nil {
		return nil, err
	}
	return obj, nil
}
