# **Onboarding Guide for ODH Operator Modules**

This document outlines the requirements and architectural standards for onboarding new modules to the Open Data Hub (ODH) Operator.

## **1\. Architectural principles & separation of concerns**

To maintain scalability and decouple lifecycles, the architecture enforces a strict separation of concerns between the **ODH Operator** (control plane) and the **module controller** (formerly companion controller).

### **Design Philosophy**

The primary driver for this architecture is to ensure modules are **as independent as possible**.

* **Standalone Design:** You should design your Module Controller as if it were a completely independent operator capable of running on a cluster without the ODH Operator.  
* **Self-Sufficiency:** It must contain all the logic, manifests, and intelligence required to manage the lifecycle of its module.  
* **Orchestration vs. Management:** The ODH Operator is merely an **orchestrator** that installs your operator. It does not "run" your module; your operator does.  
* **Upstream & Extensibility:** The Module Controller could also act as an **extender** for the upstream component. Instead of modifying the upstream codebase to add platform-specific modules (which creates maintenance debt). The controller can implement this logic **directly** (e.g., by reconciling specific platform resources) or by deploying **sub-controllers**, **sidecars**, etc.  
  **Note:** if not absolutely necessary prefer extending upstream components instead of modifying upstream code downstream

### **The ODH Operator (orchestrator)**

Acts as the central management interface. It does **not** manage the deep internal resources of the module (e.g., the specific Pods, Services, or Routes of the application).

**Responsibility**

* It watches the platform CRs (such as `DataScienceCluster`, `DSCInitialization`, `Auth`, `GatewayConfig`, `Monitoring`).  
* It deploys the **module controllers** (Deployment, RBAC, etc.).  
* Creates/Updates the **module CRs** based on the DSC configuration.  
* Watches all the created resources (CRs, Deployments, etc) having the **components.platform.opendatahub.io/managed-by** label  
* Aggregates status from the module CRs back to the DSC.  
* Prunes owned module resources and controllers when the models is not configured, handles module removal in case of upgrades

### **The module controller**

The domain expert for the specific module.

**Responsibility**

* Reconciles the **module CR**.  
* **Installation:** Owns the manifest lifecycle (install, upgrade, delete) for the actual application.  
* **Environment detection:** Auto-detects cluster states (e.g., FIPS mode, disconnected environments) and adjusts the installation accordingly.  
* **Status reporting:** Reports granular health and provisioning status back to the module CR.

## **2\. API requirements (CRD)**

Each module must provide a high-level custom resource definition (CRD).

### **2.1 Scope and metadata**

* **Scope:** Cluster  
* **Cardinality:** singleton (The system expects a single instance per cluster).  
* **Naming Enforcement:** To enforce the singleton pattern, the CRD **must** strictly validate the `metadata.name`. Use a CEL validation rule (preferred) or a Validating Webhook to ensure the name can **only** be a specific reserved string (e.g., `default`, `model-registry`, etc).  
  * **Example CEL Rule:** `self.metadata.name == 'default'`  
* **Group:** should use `components.platform.opendatahub.io` or `services.platform.opendatahub.io`.  
* **Version:** Must match the support level of the module:  
  * **Developer preview:** Must use `vXalphaY` (e.g., `v1alpha1` for the first version, `v2alpha1` for a preview of version 2).  
  * **Technology preview:** Must use `vXbetaY` (e.g., `v1beta1`, `v2beta1`).  
  * **General availability (GA):** Must use `vX` (e.g., `v1`, `v2`).

### **2.2 Spec configuration**

The CRD `spec` is the **primary source of truth** for all functional configuration. It must adhere to standard operational patterns.

**Defaults & Validation:**

* **Requirement:** Components must strive for a "zero-config" experience. Every optional field should have a sensible **default value** that results in a working configuration.  
* **Enforcement:** If a field is mandatory or requires specific formatting, strict **validation logic** (via OpenAPI schema enums, regex, or CEL validation rules) must be implemented to provide immediate feedback to the user.

**Platform-Managed Fields (Internal APIs):**

Certain global platform configurations (e.g., `Observability`, `Certificates`, `Auth`) are defined centrally in the **one of the platform CR** (such as the DSC) or are enforced by platform policy. The ODH Operator reads these platform settings and **projects** them into specific fields in your Module CR `spec`, continuously reconciling updates and strictly **reverting** any manual user edits to ensure platform compliance.

* **Requirement:** Your Module CRD must expose these fields. For example, if your module needs Authentication, expose a `spec.auth` struct. The ODH Operator will populate it.  
* **Consistency:** Do **not** use the ConfigMap for these settings.

### **2.3 Status specification (`PlatformObject`)**

The CRD status must adhere to the `PlatformObject` interface pattern to ensure the ODH operator can parse it generically.

**Required status fields:**

* `observedGeneration` (int64): The last generation observed by the controller.  
* `conditions` (\[\]metav1.Condition): A list of standard Kubernetes conditions.  
* `releases` (Array of Objects): A list of installed components.  
  * `name` (string): The name of the component.  
  * `repoUrl` (string): The repository url of the component.  
  * `version` (string): The version of the component.

**Mandatory conditions:**

* `Ready`: The top-level aggregate status.  
  * `True`: The module is fully functional and available for use.  
  * `False`: The module is unhealthy, installing, or has failed to provision.  
* `ProvisioningSucceeded`:  
  * `True`: The underlying manifests (Deployments, Services) were successfully applied.  
  * `False`: An error occurred during manifest application.  
  * **Aggregation:** **MUST** be aggregated into `Ready`.  
* `Degraded`:  
  * `True`: The module is functioning but in a degraded state.  
  * `False`: The module is operating normally with no warnings.

**Semantics & Examples:**

* **Ready=True, Degraded=True (Partial Availability):** The main service is up, but a non-critical sub-component is failing, for example the Dashboard UI is accessible (Ready), but the metrics collector service is crash-looping (Degraded). Users can still work, but observability is lost/degraded.  
* **Ready=False (Unusable):** The main service is down or a critical dependency is missing, for example, the Dashboard UI Deployment is 0/1 replicas. The module is not usable.  
* **Aggregation:** **COULD** be aggregated into `Ready`, depending on the severity. If the degradation renders the module unusable, it should set `Ready=False`. If it is a minor warning, `Ready` can remain `True`.

### **2.4 Configuration via ConfigMap (Strictly Minimal)**

The ConfigMap should be kept **strictly minimal**. It is reserved for environmental overrides and hidden flags, **not** for standard application configuration.

**What belongs in the CR `spec`:**

* **User-configurable settings:** (e.g., DB connection pool size, ports, storage classes).  
* **Platform settings:** (e.g., Auth configuration, Certificates).

**What belongs in the ConfigMap:**

* **Internal Module Flags:** These flags are used to configure the behavior of the controller and should not contain APIs.

**Lifecycle & Enforcement:**

* **Out-of-the-Box Defaults:** The Module Controller manifests **must** include this ConfigMap with sensible default values. This allows module developers to ship, for example, the component with specific module flags enabled or disabled by default.  
* **ODH Operator Responsibility:** The ODH Operator applies this ConfigMap during installation. It then **enforces** its state; if a user attempts to manually modify these flags, the ODH Operator will revert the changes to ensure platform consistency and supportability.  
* **Module Controller Responsibility:** It is entirely up to the Module Controller to decide **how** to consume this ConfigMap (e.g., mounting it as a volume, watching it for changes and reconciling, polling, or restarting pods). This guide does **not** prescribe a specific mechanism; this is an implementation detail left to the module controller's discretion.

## **3\. Implementation requirements**

### **3.1 Allowed manifests**

The ODH operator will only install the **module controller** manifests. The module repository must provide a directory containing **only** the **minimal set of artifacts** required to bootstrap the module controller. The manifests should strictly encompass the artifacts needed to **deploy and run the module controller** (e.g., the controller Deployment, its RBAC, and the Module CRD), do **not** include application-level manifests (e.g., ModelMesh Serving runtime, Dashboard UI Deployment). The ODH Operator is capable of rendering and applying manifests using **Helm** or **Kustomize**. Note that these manifests are **embedded** in the ODH controller binary at **build time**, ensuring the operator is self-contained and does not require runtime network access to fetch manifests.

**Notes:** 

* The actual application manifests are **embedded** within the module controller and applied by the controller, not the ODH operator. The specific manifest types (helm, kustomize, plain yaml) and technical mechanism used to embed these manifests is a decision left to the module team, as long as the controller remains self-contained.  
* The **Helm** support is limited to templates rendering only as first stage, support for advanced features such as hooks and other helm specific features will be evaluated at later stage 

### **3.2 Logic & detection**

The module controller is responsible for "smart" behavior (a dedicated set of functionalities will be provided in the form of go modules, see [Shared Utilities Repository](#6.2-shared-utilities-repository)) , for example, the module controller could check if the cluster has FIPS enabled and switch internal crypto libraries; the module must not rely on the ODH operator to do "smart" behavior and pass that down.

### **3.3 Dependency management**

Modules must discover dependencies dynamically by querying the Kubernetes API for the existence and status of other Module/Component CRs.

The module controller must handle missing dependencies gracefully.

* **Optional dependency:** If missing, disable the related functionality and update the `Degraded` condition if necessary, but keep `Ready=True`.  
* **Critical dependency:** If a required dependency is missing, set `Ready=False` (with a clear Reason) or `Degraded=True`, but do **not** crash the controller loop. Wait for the dependency to appear.

### **3.4 Internal Certificate Management** 

Many modules require internal TLS certificates, particularly for **Admission Webhooks** or mTLS between components.

* The Module Controller should default to using **cert-manager** (we will avoid dependency on OpenShift serving certs intentionally) to provision and rotate certificates for webhooks and internal services. This ensures standard lifecycle management.

## **4\. Integration with DataScienceCluster (DSC)**

The `DataScienceCluster` (DSC) CR is the user-facing entry point.

* The module has a stanza in the DSC i.e. `spec.components`.  
* The ODH operator reads `spec.components.mymodule` from the DSC and projects it into the `MyModule` CR.  
* The `MyModule` CR is free to support additional `spec` fields that are **not** exposed in the DSC. This allows advanced configuration by editing the `MyModule` CR directly.  
  * *Example:* Users may need to fine-tune **resource requirements** (CPU/Memory requests and limits) for the deployed controller's Pods. While these operational details are too granular for the high-level DSC, they can be exposed in the `MyModule` CR (e.g., via `spec.controllers[].resources`), allowing administrators to adjust them directly on the module level.  
    

**Notes:** 

* The exact machinery on how the module stanza is still to be defined, ideally it should be auto generated using some module manifest (such has Helm values jsonschema)

## **5\. Example reference**

### **5.1 The Module CRD**

Below is an example of what the `ModelRegistry` module CRD might look like.

```yaml
apiVersion: components.platform.opendatahub.io/v1alpha1 
kind: ModelRegistry 
metadata: 
  name: model-registry-default 
spec: 
  # General Management State (Managed, Unmanaged) 
  # Default: Managed 
  managementState: Managed 
  
  # Platform-Managed API (Populated by ODH Operator) 
  # Do not configure via ConfigMap! 
  auth: 
    enabled: true 
    audiences:
    - https://kubernetes.default.svc
  
  # Module specific configuration 
  grpcPort: 9090 
  restPort: 8080 
  
  # Advanced config NOT necessarily exposed in DSC 
  controllers:
  - name: model-registry-controller
    resources:
      requests:
        cpu: "100m"
        memory: "512Mi"
      limits:
        cpu: "500m"
        memory: "1Gi"

status: 
  observedGeneration: 1 
  
  # Release information 
  releases: 
  - name: "model-registry"
    repoUrl: "https://github.com/kubeflow/model-registry"
    version: "v2.0.1" 

  conditions: 
  - type: Ready 
    status: "True" 
    reason: "Ready" 
    message: "All model registry components are running." 
    
  - type: ProvisioningSucceeded 
    status: "True" 
    reason: "ProvisioningComplete" 
    message: "Manifests applied successfully." 
    
  - type: Degraded 
    status: "True" 
    reason: "MissingOptionalDB" 
    message: "External DB not found, falling back to local SQLite. Performance may be impacted." 
```

### **5.2 The ConfigMap (image & module configuration)**

The ODH operator creates a ConfigMap (e.g., `model-registry-config`) that the module controller mounts or reads.

```yaml
apiVersion: v1
kind: ConfigMap
metadata:
  name: odh-model-registry-config
  namespace: opendatahub
data:
  # controller (user configurable)
  controller.yaml: |
    zap:
      level: info
    pprof:
      enable: true
  # platform (injected)
  platform.yaml: |
    platform:
      distribution: openshift
      flavor: managed
      version: 3.0.0
```

## **6\. Development Flexibility & Shared Utilities**

### **6.1 Implementation Freedom**

As long as the Module Controller adheres to the expectations and architectural contracts described in this guide (CRD API, Status reporting, separation of concerns), developers are **free to implement the operator following the rules and patterns they prefer**. The strictness of this guide applies to the **interfaces** (how the ODH Operator interacts with your module), not the internal implementation details of your controller logic.

### **6.2 Shared Utilities Repository** {#6.2-shared-utilities-repository}

To facilitate the development of operators, the OpenShift AI Core Platform team will create and maintain a shared repository containing code extracted from the current ODH Operator.

This repository aims to accelerate development by providing **common logic (go modules)** for standard tasks, including:

* Utilities for processing Kubernetes manifests, including support for:  
  * Kustomize  
  * Go Templates  
  * Helm rendering  
  * Plain YAML  
* Helpers for managing standard status conditions (Ready, Degraded, etc.).  
* Standard patterns for deploying resources and ensuring clean removal, including:  
  * Logic to allow partial modification of deployments (e.g., replicas and resources).  
  * Annotation handling to make resources unmanaged and modifiable by the user.  
  * Common labels/annotations injection.  
  * Common logic to remove leftovers on upgrades or configuration changes.  
* Helpers to enable “smart” behaviors, i.e:  
  * Detection of the actual Kubernetes distribution (OpenShift, AKS, EKS, etc)  
  * Detection of FIPS mode  
  * Etc  
* Helpers to read controller configuration  
* Helpers to monitor dependency operators conditions

**Note:** Usage of this shared code is optional but highly recommended to reduce boilerplate and ensure consistency across modules.
