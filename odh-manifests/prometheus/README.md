# Prometheus

Prometheus component subscribes to Prometheus operator, which in turn issues an install of the Prometheus community operator in the namespace. This component also creates two types of instances:
1. Prometheus instance. This creates a prometheus pod that looks for `ServiceMonitors` with label
```yaml
  labels:
    team: opendatahub
```
2. Service Monitor instances. This creates `ServiceMonitor` instances to  monitor specific services that expose prometheus metric endpoints. At this time there are two Service Monitors, one for services with port name `web` and one for kafka.
 
### Folders
There are two main folders in the Prometheus component
1. cluster: contains the subscription to the operator
2. operator: contains the routes and instances of the operator

### Prometheus Portal

This installation creates a route to the Prometheus portal. To access the portal go to `Routes` and click on the `prometheus' route.


### Installation
To install Prometheus add the following to the `kfctl` yaml file.

```yaml
  - kustomizeConfig:
      parameters:
      - name: namespace
        value: opendatahub
      repoRef:
        name: manifests
        path: prometheus/cluster
    name: prometheus-cluster
  - kustomizeConfig:
      parameters:
      - name: namespace
        value: opendatahub
      repoRef:
        name: manifests
        path: prometheus/operator
    name: prometheus-operator
```
