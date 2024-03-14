package fixtures

const ServiceMeshControlPlaneCRD = `apiVersion: apiextensions.k8s.io/v1
kind: CustomResourceDefinition
metadata:
  labels:
    maistra-version: 2.4.2
  annotations:
    service.beta.openshift.io/inject-cabundle: "true"
    controller-gen.kubebuilder.io/version: v0.4.1
  name: servicemeshcontrolplanes.maistra.io
spec:
  group: maistra.io
  names:
    categories:
      - maistra-io
    kind: ServiceMeshControlPlane
    listKind: ServiceMeshControlPlaneList
    plural: servicemeshcontrolplanes
    shortNames:
      - smcp
    singular: servicemeshcontrolplane
  scope: Namespaced
  versions:
    - name: v1
      schema:
        openAPIV3Schema:
          type: object
          x-kubernetes-preserve-unknown-fields: true
      served: true
      storage: false
      subresources:
        status: {}
    - name: v2
      schema:
        openAPIV3Schema:
          type: object
          x-kubernetes-preserve-unknown-fields: true
      served: true
      storage: true
      subresources:
        status: {}
`

const OssmSubscription = `apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: servicemeshoperator
  namespace: openshift-operators
spec:
  channel: stable
  installPlanApproval: Automatic
  name: servicemeshoperator
  source: redhat-operators
  sourceNamespace: openshift-marketplace
  startingCSV: servicemeshoperator.v2.4.5
`
