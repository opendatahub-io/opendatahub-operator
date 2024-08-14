# Feature DSL

`Feature` struct encapsulates a set of resources to be created and actions to be performed in the cluster in order to enable desired functionality. For example, this could involve deploying a service or configuring the cluster infrastructure necessary for a component to function properly.

## Goals

- Abstraction for self-contained units of desired changes in the cluster.
- Granular control of resource management.
- Intuitive way of defining desired cluster changes in the controller code.
- Support both programmatic and declarative means of changing cluster state.
- Detailed status reporting enables quick diagnosis of issues.

## Overview

```mermaid
---
title: Feature Domain Model
---
classDiagram
    direction LR
    
    namespace Controller {
        class Feature {
            <<struct>>
            Precondtions <1>
            Manifests <2>
            Resources <3>
            PostConditions <4>
            CleanupHooks <5>
            Data <6>
        }


        class FeatureTracker {
            <<struct>>
        }    
    }
    
    namespace Cluster {
        class Resource {
            <<k8s>>
        }
    }
    
    Feature -- FeatureTracker: is associated with
    FeatureTracker --> "1..*" Resource: owns
    
    note for FeatureTracker "Internal Custom Resource (not part of user-facing API)"
```

- `<1>` Preconditions which are needed for the feature to be successfully applied (e.g. existence of particular CRDs)
> [!NOTE] 
> Although not strictly required due to the declarative nature of Kubernetes, it can improve error reporting and prevent the application of resources that would lead to subsequent errors.
- `<2>` Manifests (i.e. YAML files) to be applied on the cluster. These can be arbitrary files or Go templates.
- `<3>` Programmatic resource creation required by the feature.
- `<4>` Post-creation checks, for example waiting for pods to be ready.
- `<5>` Cleanup hooks (i.e. undoing patch changes)
- `<6>` Data store needed by templates and actions (functions) performed during feature's lifecycle.

## Creating a Feature

### Getting started

The easiest way to define a feature is by using the Feature Builder (builder.go), which provides a guided flow through a fluent API, making the construction process straightforward and intuitive.

```golang

controlPlane, errControlPlane := servicemesh.FeatureData.ControlPlane.Create(ctx, r.Client, &instance.Spec)
if errControlPlane != nil {
    return fmt.Errorf("failed to create control plane feature data: %w", errControlPlane)
}

smcp, errSMCPCreate := feature.Define("mesh-control-plane-creation").
	TargetNamespace("opendatahub").
	ManifestsLocation(Templates.Location).
	Manifests(
		path.Join(Templates.ServiceMeshDir),
	).
	WithData(controlPlane).
	PreConditions(
		servicemesh.EnsureServiceMeshOperatorInstalled,
		feature.CreateNamespaceIfNotExists(serviceMeshSpec.ControlPlane.Namespace),
	).
	PostConditions(
		feature.WaitForPodsToBeReady(serviceMeshSpec.ControlPlane.Namespace),
	).
	Create()
```

For more examples have a look at `integration/feature` tests.

### Enhancing the Feature with data structure

Features can rely on additional data structures to be used when creating resources, processing templates or performing checks like preconditions. 

Static values can be supplied by defining key-value pairs in the Feature's context data:

```golang
WithData(
    feature.Value("Secret", "static-secret"),
)
```
This can be later accessed in the templates  `{{ .Secret }}` or Feature's Action functions:

```golang
secret := f.Get[string]("Secret")
```

It may be necessary to supply data that is only available at runtime, such as cluster configuration details. 
For this purpose, a `Provider` can be used:

```golang
WithData(
    feature.Provider("Domain", func() (string, error) {
		//... fetch the domain somehow
		return domain, nil
    }),
)
```

For more on how to further simplify re-use of Feature's context data see a [dedicated section about conventions](#feature-context-re-use).

## Execution flow 

The diagram below depicts the flow when Feature is applied.

```mermaid
flowchart TD
    Data[Resolve Feature Data]
    PC[Pre-condition check]
    R[Create Resources]
    M[Apply manifests]
    P[Post-condition check]
    E(((Error)))
    S(((Feature applied)))
    
    Data --> OK1{OK} 
    OK1 -->|No| E
    OK1 -->|Yes|PC
    PC --> OK2{OK}
    OK2 -->|No| E
    OK2 -->|Yes| R
    R --> M
    M --> OK3{OK}
    OK3 -->|No| E
    OK3 -->|Yes| P
    P --> OK4{OK}
    OK4 -->|Yes| S
    OK4 -->|No| E
```

## Feature Tracker

`FeatureTracker` is an internal CRD, not intended to be used in user-facing API. Its primary goal is to establish ownership of all resources that are part of the given feature. This way we can transparently
garbage collect them when feature is no longer needed.

Additionally, it updates the `.status`  field with detailed information about the Feature's lifecycle operations. This can be useful for troubleshooting, as it indicates which part of the feature application process is failing.

## Managing Features with `FeaturesHandler`

The `FeaturesHandler` (`handler.go`) provides a structured way to manage and coordinate the creation, application, and deletion of features needed in particular Data Science Cluster configuration such as cluster setup or component configuration.

When creating a `FeaturesHandler`, developers can provide a FeaturesProvider implementations. This allows for the straightforward registration of a list of features that the handler will manage.

## Conventions

### Templates

Golang templating can be used to create (or patch) resources in the cluster which are part of the Feature. Following rules are applied:

* Any file which has `.tmpl.` in its name will be treated as a template for the target resource.
* Any file which has `.patch.` in its name will be treated a patch operation for the target resource.

By convention, these files can be stored in the resources folder next to the Feature setup code, so they can be embedded as an embedded filesystem when defining a feature, for example, by using the Builder. 

Anonymous struct can be used on per feature set basis to organize resource access easier:

```golang
//go:embed resources
var resourcesFS embed.FS

const baseDir = "resources"

var Resources = struct {
	// InstallDir is the path to the Serving install templates.
	InstallDir string
	// Location specifies the file system that contains the templates to be used.
	Location fs.FS
	// BaseDir is the path to the base of the embedded FS
	BaseDir string
}{
	InstallDir:     path.Join(baseDir, "installation"),
	Location:       resourcesFS,
	BaseDir:        baseDir,
}
```

### Feature context re-use

The `FeatureData` anonymous struct convention provides a consistent way to manage data for features.

By defining data and extraction functions, it simplifies handling of feature-related data in both templates and 
functions where this data is required. 

```golang

const (
    servingKey = "Serving" // <1>
)

// FeatureData is a convention to simplify how the data for the Serverless features is Defined and accessed.
var FeatureData = struct {
    Serving feature.DataDefinition[*infrav1.ServingSpec, ServingData] // <2>
}{
    Serving: feature.DataDefinition[*infrav1.ServingSpec, ServingData]{
        Create:  CreateServingConfig, // <3>
        Extract: feature.ExtractEntry[ServingData](servingKey), // <4>
    },
}

type ServingData struct { // <5>
    KnativeCertificateSecret string
    KnativeIngessDomain string
}

func CreateServingConfig(ctx context.Context, cli client.Client, source *infrav1.ServingSpec) (ServingData, error) {
    certificateName := provider.ValueOf(source.IngressGateway.Certificate.SecretName).OrElse(DefaultCertificateSecretName) // <5>
    domain, errGet := provider.ValueOf(source.IngressGateway.Domain).OrGet(func() (string, error) {
        return KnativeDomain(ctx, cli)
    }).Get() // <6>
    if errGet != nil {
        return ServingData{}, fmt.Errorf("failed to get domain for Knative: %w", errGet)
    }

    config := ServingData{
        KnativeCertificateSecret: certificateName,
        KnativeIngessDomain:      domain,
    }

    return config, nil
}

var _ feature.Entry = &ServingData{} // <7>

func (s ServingData) AddAsEntry(f *feature.Feature) error {
    return f.Set(servingKey, s)
}

func KnativeDomain(ctx context.Context, c client.Client) (string, error) {
    var errDomain error
    domain, errDomain := cluster.GetDomain(ctx, c)
    if errDomain != nil {
        return "", fmt.Errorf("failed to fetch OpenShift domain to generate certificate for Serverless: %w", errDomain)
    }

    domain = "*." + domain
    
	return domain, nil
}
```
- `<1>` Key used to store defined data entry in the Feature's context. This can be later used in templates (`{{ .Serving }}`) as well as golang functions.
- `<2>` Generic struct used to define how Feature's data entry is created. Parametrized types hold information about the source and target types. The source type represents the origin from which data is taken, defining the type of data being input or extracted from the source. The target type determines the type of the value that will be stored in the key-value store (feature context data).
- `<3>` Constructor function that creates the data needed for the feature. Uses passed source object for mapping. With passed context and client it also can fetch additional data if needed.
- `<4>` Defines how to extract the value from the context (typed access instead of relying on string keys).
- `<5>` Data structure that holds the data needed for the feature. Can be also used in other parts of the system
- `<6>` Example of how to use the provider package to get the value from the source or fallback to a function call.
- `<7>` Implementing the `feature.Entry` interface allows the data structure to be stored in the Feature's context.

Example below illustrates both storing data in the feature and accessing it from Feature's action function:

```golang

serving, errCreate := serverless.FeatureData.Serving.Create(ctx, r.Client, &instance.Spec)
if errCreate != nil {
    return fmt.Errorf("failed to create serving feature data: %w", errCreate)
}

// Define the feature using builder
// ...
WithData(serving)
// ...

// To extract in the action function
func DoSomethingWithServingData(f *feature.Feature) (string, error) {
    serving, err := FeatureData.Serving.Extract(f);
    if err != nil {
        return "", err
    }
    // do something with serving data - create a resource, etc.
    // ....
}
```

