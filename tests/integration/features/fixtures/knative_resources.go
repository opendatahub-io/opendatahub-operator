package fixtures

const (
	TestNamespacePrefix = "test-ns"
	TestDomainFooCom    = "*.foo.com"
)

const KnativeServingSubscription = `apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: serverless-operator
  namespace: openshift-serverless
spec:
  channel: stable
  installPlanApproval: Automatic
  name: serverless-operator
  source: redhat-operators
  sourceNamespace: openshift-marketplace
  startingCSV: serverless-operator.v1.31.0
`
const KnativeServingCrd = `apiVersion: apiextensions.k8s.io/v1beta1
kind: CustomResourceDefinition
metadata:
  name: knativeservings.operator.knative.dev
spec:
  group: operator.knative.dev
  names:
    kind: KnativeServing
    listKind: KnativeServingList
    plural: knativeservings
    singular: knativeserving
  scope: Namespaced
  versions:
  - name: v1beta1
    served: true
    storage: true
    schema:
      openAPIV3Schema:
        type: object
`

const KnativeServingInstance = `apiVersion: operator.knative.dev/v1beta1
kind: KnativeServing
metadata:
  name: knative-serving-instance
spec: {}
`

const OpenshiftClusterIngress = `apiVersion: config.openshift.io/v1
kind: Ingress
metadata:
  name: cluster
spec:
  domain: "foo.io"
  loadBalancer:
    platform:
      type: ""`
