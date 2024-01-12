# Open Data Hub (ODH) Installation Guide with OpenShift Service Mesh (OSSM)

This guide will walk you through the installation of Open Data Hub with OpenShift Service Mesh.

## Prerequisites

* OpenShift cluster
* Command Line Interface (CLI) tools
  * `kubectl`
  * `operator-sdk` v1.24.1 (until operator changes are merged - to build new bundles)

* Pre-installed operators
  * Openshift Service Mesh
  * Authorino
  * Open Data Hub

* Service Mesh Control Plane configured
  
### Check Installed Operators

You can use the following command to verify that all required operators are installed:

```sh
kubectl get operators | awk -v RS= '/servicemesh/ && /opendatahub/ && /authorino/ {exit 0} {exit 1}' || echo "Please install all required operators."
```

#### Install Required Operators

The `createSubscription` function can be used to simplify the installation of required operators:

```sh
createSubscription() {
  local name=$1
  local source=${2:-"redhat-operators"}
  local channel=${3:-"stable"}

  echo  "Create Subscription resource for $name"
  eval "kubectl apply -f - <<EOF
apiVersion: operators.coreos.com/v1alpha1
kind: Subscription
metadata:
  name: $name
  namespace: openshift-operators
spec:
  channel: $channel
  installPlanApproval: Automatic
  name: $name
  source: $source
  sourceNamespace: openshift-marketplace
EOF"    
}
```

You can use the function above to install all required operators:

```sh
createSubscription "servicemeshoperator"
createSubscription "authorino-operator" "community-operators" "alpha"
```

or use Openshift Console to click through the UI.

To run pre-built image of this PR simply execute:
```sh
operator-sdk run bundle quay.io/maistra-dev/opendatahub-operator-bundle:v1.0.0-service-mesh --namespace openshift-operators --timeout 5m0s
```

or go to [README.md#Deploying operator locally](../README.md#deployment) to learn how to build and test changes done locally.

> **Warning**
>
> You may need to manually update the installation of the Authorino operator via the Installed Operators tab in the OpenShift Console.


> **Warning**
>
> Please ensure that the Service Mesh Control Plane is properly configured as we apply patches to it. It is assumed that the installation has already been done.


For example, the following commands configure a slimmed-down profile:

```sh
kubectl create ns istio-system
kubectl apply -n istio-system -f -<<EOF
apiVersion: maistra.io/v2
kind: ServiceMeshControlPlane
metadata:
  name: minimal
spec:
  version: v2.4
  tracing:
    type: None
  addons:
    prometheus:
      enabled: false
    grafana:
      enabled: false
    jaeger:
      name: jaeger
    kiali:
      name: kiali
      enabled: false
EOF
```

## Setting up Open Data Hub Project

Create the Data Science Cluster Initialization. The following commands will create a file called `dsci.ign.yaml` [^1]:


[^1] If you are wondering why `.ign.` - it can be used as global `.gitignore` pattern so you won't be able commit such files.

```sh
cat <<'EOF' > dsci.ign.yaml
apiVersion: dscinitialization.opendatahub.io/v1
kind: DSCInitialization
metadata:
  name: default
spec:
  applicationsNamespace: opendatahub
  monitoring:
    managementState: Managed
    namespace: opendatahub
  serviceMesh:
    managementState: Managed # (1)
    mesh:
      name: minimal # (2)
      certificate:
        generate: true # (3)
EOF
```

* **(1)**: setting this value will enable Service Mesh support for Open Data Hub
* **(2)**: name of Service Mesh Control Plane (defaults to `basic`)
* **(3)**: instructs operator to generate self-signed certificate

This will instruct the DSCI controller to perform following steps:

- Deploys an instance of Authorino controller which will handle all `AuthConfig`s for ODH (using certain label)
- Registers Authorino as external authorization provider for Istio
- Configure Envoy filters to handle OAuth2 flow for Dashboard
- Create Gateway route for Dashboard
- Create necessary Istio resources such as Virtual Services, Gateways and Policies

Next, create DataScienceCluster with managed `Dashboard` and `Workbenches` components:

```sh
apiVersion: datasciencecluster.opendatahub.io/v1
kind: DataScienceCluster
metadata:
  name: default
spec:
  components:
    dashboard:
      managementState: "Managed"
    workbenches:
      managementState: "Managed"
```
`
> **Warning**
>
> Other components are not supported yet.

Go to the Open Data Hub dashboard in the browser:

```sh
export ODH_ROUTE=$(kubectl get route --all-namespaces -l maistra.io/gateway-name=odh-gateway -o yaml | yq '.items[].spec.host')

xdg-open https://$ODH_ROUTE > /dev/null 2>&1 &    
```

## Troubleshooting

### Audience-aware tokens

Audience-aware token authenticators will verify that the token was intended for at least one of the audiences in this list. This can be crucial for environments such as ROSA. If no audiences are provided, the audience will default to the audience of the Kubernetes apiserver (`kubernetes.default.svc`). In order to change it, you should first know the token audience of your `serviceaccount`.

```shell
TOKEN=YOUR_USER_TOKEN
ODH_NS=opendatahub
kubectl create -o jsonpath='{.status.audiences[0]}' -f -<<EOF
apiVersion: authentication.k8s.io/v1
kind: TokenReview
spec:
  token: "$TOKEN"
  audiences: []
EOF
```
Next, use the output to configure `DSCInitialization`:

```yaml
apiVersion: dscinitialization.opendatahub.io/v1
kind: DSCInitialization
metadata:
  name: default
spec:
  applicationsNamespace: opendatahub
  monitoring:
    managementState: Managed
    namespace: opendatahub
  serviceMesh:
    managementState: Managed
    auth:
      authorino:
        audiences:
          - https://rh-oidc.s3.us-east-1.amazonaws.com/24ku4l2qor82cfscmagr6h3g4r3s0r2d # (1)
    mesh:
      name: minimal
      certificate:
        generate: true
```

**(1)** this will be used to configure in Authrino's `AuthConfig`

### Issue: `OAuth flow failed`

Start by checking the logs of `openshift-authentication` pod(s):

```sh
kubectl logs $(kubectl get pod -l app=oauth-openshift -n openshift-authentication -o name) -n openshift-authentication  
```

This can reveal errors like:

* Wrong redirect URL
* Mismatching secret between what OAuth client has defined and what is loaded for Envoy Filters.

If the latter is the case (i.e., an error like `E0328 18:39:56.277217 1 access.go:177] osin: error=unauthorized_client, internal_error=<nil> get_client=client check failed, client_id=${ODH_NS}-oauth2-client`)`, check if the token is the same everywhere by comparing the output of the following commands:


```sh
kubectl get oauthclient.oauth.openshift.io ${ODH_NS}-oauth2-client
kubectl exec $(kubectl get pods -n istio-system -l app=istio-ingressgateway  -o jsonpath='{.items[*].metadata.name}') -n istio-system -c istio-proxy -- cat /etc/istio/${ODH_NS}-oauth2-tokens/token-secret.yaml
kubectl get secret ${ODH_NS}-oauth2-tokens -n istio-system -o yaml
```
To read the actual value of secrets you could use a [`kubectl` plugin](https://github.com/elsesiy/kubectl-view-secret) instead. Then the last line would look as follows `kubectl view-secret ${ODH_NS}-oauth2-tokens -n istio-system -a`.

The `i`stio-ingressgateway` pod might be out of sync (and so `EnvoyFilter` responsible for OAuth2 flow). Check its logs and consider restarting it:

```sh
kubectl rollout restart deployment -n istio-system istio-ingressgateway  
```
