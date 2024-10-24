package plugins_test

import (
	"sigs.k8s.io/kustomize/api/provider"
	"sigs.k8s.io/kustomize/api/resource"

	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/cluster/gvk"
	"github.com/opendatahub-io/opendatahub-operator/v2/pkg/plugins"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

var factory = provider.NewDefaultDepProvider().GetResourceFactory()

var _ = Describe("Remover plugin", func() {
	var res *resource.Resource

	BeforeEach(func() {
		var err error
		res, err = factory.FromBytes([]byte(`
apiVersion: apps/v1
kind: Deployment
metadata:
  name: testdeployment
spec:
  replicas: 3
  selector:
    matchLabels:
      control-plane: odh-component
  template:
    metadata:
      labels:
        app: odh-component
        app.opendatahub.io/odh-component: "true"
        control-plane: odh-component
    spec:
      containers:
      - name: conatiner0
        env:
        - name: NAMESPACE
          value: namespace
        image: quay.io/opendatahub/odh-component:latest
        ports:
        - containerPort: 80
      - name: conatiner1
        env:
        - name: NAMESPACE
          value: namespace
        image: quay.io/opendatahub/odh-component:latest
        ports:
        - containerPort: 8080
        resources:
          limits:
            cpu: 500m
            memory: 2Gi
          requests:
            cpu: 10m
            memory: 64Mi

`))
		Expect(err).NotTo(HaveOccurred())
	})

	It("Should be able to remove replicas from resources", func() {
		rmPlug := plugins.RemoverPlugin{
			Gvk:  gvk.Deployment,
			Path: []string{"spec", "replicas"},
		}

		expected := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: testdeployment
spec:
  selector:
    matchLabels:
      control-plane: odh-component
  template:
    metadata:
      labels:
        app: odh-component
        app.opendatahub.io/odh-component: "true"
        control-plane: odh-component
    spec:
      containers:
      - name: conatiner0
        env:
        - name: NAMESPACE
          value: namespace
        image: quay.io/opendatahub/odh-component:latest
        ports:
        - containerPort: 80
      - name: conatiner1
        env:
        - name: NAMESPACE
          value: namespace
        image: quay.io/opendatahub/odh-component:latest
        ports:
        - containerPort: 8080
        resources:
          limits:
            cpu: 500m
            memory: 2Gi
          requests:
            cpu: 10m
            memory: 64Mi

`
		err := rmPlug.TransformResource(res)
		Expect(err).NotTo(HaveOccurred())

		Expect(res.MustYaml()).To(MatchYAML(expected))
	})

	It("Should be able to remove resources filed", func() {
		rmPlug := plugins.RemoverPlugin{
			Gvk:  gvk.Deployment,
			Path: []string{"spec", "template", "spec", "containers", "*", "resources"},
		}

		expected := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: testdeployment
spec:
  replicas: 3
  selector:
    matchLabels:
      control-plane: odh-component
  template:
    metadata:
      labels:
        app: odh-component
        app.opendatahub.io/odh-component: "true"
        control-plane: odh-component
    spec:
      containers:
      - name: conatiner0
        env:
        - name: NAMESPACE
          value: namespace
        image: quay.io/opendatahub/odh-component:latest
        ports:
        - containerPort: 80
      - name: conatiner1
        env:
        - name: NAMESPACE
          value: namespace
        image: quay.io/opendatahub/odh-component:latest
        ports:
        - containerPort: 8080
`
		err := rmPlug.TransformResource(res)
		Expect(err).NotTo(HaveOccurred())

		Expect(res.MustYaml()).To(MatchYAML(expected))
	})

	It("Should be able to remove both replicas and resources", func() {
		rmPlugRep := plugins.RemoverPlugin{
			Gvk:  gvk.Deployment,
			Path: []string{"spec", "replicas"},
		}
		rmPlugRes := plugins.RemoverPlugin{
			Gvk:  gvk.Deployment,
			Path: []string{"spec", "template", "spec", "containers", "*", "resources"},
		}

		expected := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: testdeployment
spec:
  selector:
    matchLabels:
      control-plane: odh-component
  template:
    metadata:
      labels:
        app: odh-component
        app.opendatahub.io/odh-component: "true"
        control-plane: odh-component
    spec:
      containers:
      - name: conatiner0
        env:
        - name: NAMESPACE
          value: namespace
        image: quay.io/opendatahub/odh-component:latest
        ports:
        - containerPort: 80
      - name: conatiner1
        env:
        - name: NAMESPACE
          value: namespace
        image: quay.io/opendatahub/odh-component:latest
        ports:
        - containerPort: 8080
`
		err := rmPlugRep.TransformResource(res)
		Expect(err).NotTo(HaveOccurred())

		err = rmPlugRes.TransformResource(res)
		Expect(err).NotTo(HaveOccurred())

		Expect(res.MustYaml()).To(MatchYAML(expected))
	})

	It("Should not remove fileds with unexisted path", func() {
		rmPlug := plugins.RemoverPlugin{
			Gvk:  gvk.Deployment,
			Path: []string{"spec", "unexisted"},
		}

		expected := `
apiVersion: apps/v1
kind: Deployment
metadata:
  name: testdeployment
spec:
  replicas: 3
  selector:
    matchLabels:
      control-plane: odh-component
  template:
    metadata:
      labels:
        app: odh-component
        app.opendatahub.io/odh-component: "true"
        control-plane: odh-component
    spec:
      containers:
      - name: conatiner0
        env:
        - name: NAMESPACE
          value: namespace
        image: quay.io/opendatahub/odh-component:latest
        ports:
        - containerPort: 80
      - name: conatiner1
        env:
        - name: NAMESPACE
          value: namespace
        image: quay.io/opendatahub/odh-component:latest
        ports:
        - containerPort: 8080
        resources:
          limits:
            cpu: 500m
            memory: 2Gi
          requests:
            cpu: 10m
            memory: 64Mi

`
		err := rmPlug.TransformResource(res)
		Expect(err).NotTo(HaveOccurred())

		Expect(res.MustYaml()).To(MatchYAML(expected))
	})

})
