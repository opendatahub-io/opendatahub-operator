package observability

import (
	"bytes"
	"context"
	"embed"
	"fmt"
	"path"
	"strings"
	"text/template"

	apierrs "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/util/yaml"
	"sigs.k8s.io/controller-runtime/pkg/client"

	dsciv1 "github.com/opendatahub-io/opendatahub-operator/v2/apis/dscinitialization/v1"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
)

func CreatePrometheusConfigs(ctx context.Context, cli client.Client, enabled bool, rootFS embed.FS, manifestPath string, dscispec *dsciv1.DSCInitializationSpec) error {
	foundObj := &unstructured.Unstructured{}
	var object *unstructured.Unstructured
	entries, err := rootFS.ReadDir(manifestPath)
	if err != nil {
		return fmt.Errorf("error reading dir %w", err)
	}
	for _, e := range entries {
		resourceName := strings.Split(e.Name(), ".")[0]
		foundObj := setGVK(foundObj, resourceName)
		err = cli.Get(ctx, client.ObjectKey{Name: resourceName, Namespace: dscispec.Monitoring.Namespace}, foundObj)
		if !apierrs.IsNotFound(err) {
			return fmt.Errorf("failed fetching %v CR: %w", resourceName, err)
		}
		if enabled && apierrs.IsNotFound(err) {
			object, err := updateTemplate(e.Name(), rootFS, manifestPath, dscispec)
			if err != nil {
				return fmt.Errorf("failed inject template for %s: %w", resourceName, err)
			}
			err = cli.Create(ctx, object)
			if err != nil {
				return fmt.Errorf("error creating %s: %w", resourceName, err)
			}
		}
		if !enabled && err != nil {
			err = cli.Delete(ctx, object)
			if err != nil {
				return fmt.Errorf("error removing %s: %w", resourceName, err)
			}
		}
	}
	return nil
}

func updateTemplate(fileName string, rootFS embed.FS, manifestPath string, dscispec *dsciv1.DSCInitializationSpec) (*unstructured.Unstructured, error) {
	yamlFile, err := rootFS.ReadFile(path.Join(manifestPath, fileName))
	if err != nil {
		return nil, fmt.Errorf("failed to read %s: %w", fileName, err)
	}
	tmpl, err := template.New("cr-template").Delims("[[", "]]").Parse(string(yamlFile))
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

func setGVK(foundObj *unstructured.Unstructured, resourceName string) *unstructured.Unstructured {
	switch {
	case strings.HasSuffix(resourceName, "rules"):
		foundObj.SetGroupVersionKind(gvk.PrometheusRule)
	case strings.HasSuffix(resourceName, "service-monitor"):
		foundObj.SetGroupVersionKind(gvk.ServiceMonitor)
	case strings.HasSuffix(resourceName, "pod-monitor"):
		foundObj.SetGroupVersionKind(gvk.PodMonitor)
	case strings.HasSuffix(resourceName, "scrape-config"):
		foundObj.SetGroupVersionKind(gvk.ScrapeConfig)
	}
	return foundObj
}
