package cluster

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"github.com/bitly/go-simplejson"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/gvr"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/dynamic"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

// +kubebuilder:rbac:groups="config.openshift.io",resources=ingresses,verbs=get

func GetDomain(dynamicClient dynamic.Interface) (string, error) {
	cluster, err := dynamicClient.Resource(gvr.OpenshiftIngress).Get(context.TODO(), "cluster", metav1.GetOptions{})
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

// ExtractHostNameAndPort strips given URL in string from http(s):// prefix and subsequent path,
// returning host name and port if defined (otherwise defaults to 443).
//
// This is useful when getting value from http headers (such as origin).
// If given string does not start with http(s) prefix it will be returned as is.
func ExtractHostNameAndPort(s string) (string, string, error) {
	u, err := url.Parse(s)
	if err != nil {
		return "", "", err
	}

	if u.Scheme != "http" && u.Scheme != "https" {
		return s, "", nil
	}

	hostname := u.Hostname()

	port := "443" // default for https
	if u.Scheme == "http" {
		port = "80"
	}

	if u.Port() != "" {
		port = u.Port()
		_, err := strconv.Atoi(port)
		if err != nil {
			return "", "", errors.New("invalid port number: " + port)
		}
	}

	return hostname, port, nil
}
