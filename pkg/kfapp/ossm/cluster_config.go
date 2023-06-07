package ossm

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/bitly/go-simplejson"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"net/http"
	"net/url"
	"os"
	"strings"
)

func GetDomain(config *rest.Config) (string, error) {
	dynamicClient, err := dynamic.NewForConfig(config)
	if err != nil {
		return "", nil
	}

	cluster, err := dynamicClient.Resource(
		schema.GroupVersionResource{
			Group:    "config.openshift.io",
			Version:  "v1",
			Resource: "ingresses",
		},
	).Get(context.TODO(), "cluster", metav1.GetOptions{})
	if err != nil {
		panic(err.Error())
	}

	domain, found, err := unstructured.NestedString(cluster.Object, "spec", "domain")
	if !found {
		return "", errors.New("spec.domain not found")
	}
	return domain, err
}

func GetOAuthServerDetails() (*simplejson.Json, error) {
	response, err := request(http.MethodGet, "/.well-known/oauth-authorization-server")
	if err != nil {
		return nil, err
	}

	return simplejson.NewJson(response)
}

const saCert = "/run/secrets/kubernetes.io/serviceaccount/ca.crt"

func request(method string, url string) ([]byte, error) {
	certPool, err := createCertPool()
	if err != nil {
		return nil, err
	}

	client := &http.Client{
		Transport: &http.Transport{
			TLSClientConfig: &tls.Config{
				RootCAs: certPool,
			},
		}}

	request, err := http.NewRequest(method, getKubeAPIURLWithPath(url).String(), nil)
	if err != nil {
		return nil, fmt.Errorf("failed to get api endpoint %s, error: %s", url, err)
	}

	response, err := client.Do(request)
	if err != nil {
		return nil, fmt.Errorf("failed to get api endpoint %s, error: %s", url, err)
	}

	defer response.Body.Close()

	body, err := io.ReadAll(response.Body)
	if err != nil || response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("failed to get api endpoint %s, error: %s", url, err)
	}

	return body, nil
}

func createCertPool() (*x509.CertPool, error) {
	certPool := x509.NewCertPool()
	cert, err := os.ReadFile(saCert)

	if err != nil {
		return nil, fmt.Errorf("failed to get root CA certificates: %s", err)
	}

	certPool.AppendCertsFromPEM(cert)
	return certPool, err
}

func getKubernetesServiceHost() string {
	if host := os.Getenv("KUBERNETES_SERVICE_HOST"); len(host) > 0 {
		// assume IPv6 if host contains colons
		if strings.IndexByte(host, ':') != -1 {
			host = "[" + host + "]"
		}

		return host
	}

	return "kubernetes.default.svc"
}

func getKubeAPIURLWithPath(path string) *url.URL {
	return &url.URL{
		Scheme: "https",
		Host:   getKubernetesServiceHost(),
		Path:   path,
	}
}
