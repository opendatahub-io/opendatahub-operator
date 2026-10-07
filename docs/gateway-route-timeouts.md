# Configure the shared Gateway Route timeout

In `OcpRoute` mode, configure the default shared Gateway Route's server timeout
through the existing `GatewayConfig` resource:

```yaml
apiVersion: services.platform.opendatahub.io/v1alpha1
kind: GatewayConfig
metadata:
  name: default-gateway
spec:
  ingressMode: OcpRoute
  ocpRoute:
    serverTimeout: "330s"
```

Merge these settings into the existing resource, preserving its other settings.
The operator manages the Route's `haproxy.router.openshift.io/timeout` annotation
from this field; make changes through `GatewayConfig`.

The setting applies to all components using the default shared Route, excludes
additional ingress Routes, and is inactive in `LoadBalancer` mode. It does not
change component HTTPRoute timeouts or `spec.authProxyTimeout`.

Omitting or removing the setting leaves the Route without a timeout override,
using the OpenShift router's configuration.

See the [API reference](api-overview.md#ocprouteconfig) for the field's accepted
duration format and limits. For router timeout behavior and Route annotations,
see the OpenShift [Configuring route timeouts](https://docs.redhat.com/en/documentation/openshift_container_platform/4.18/html-single/ingress_and_load_balancing/index#nw-configuring-route-timeouts_configuring-routes)
and [Route-specific annotations](https://docs.redhat.com/en/documentation/openshift_container_platform/4.18/html-single/ingress_and_load_balancing/index#nw-route-specific-annotations_configuring-routes)
documentation.
