# The base image is expected to contain
# /bin/opm (with a serve subcommand) and /bin/grpc_health_probe
FROM registry.redhat.io/openshift4/ose-operator-registry-rhel9:v4.18


# Configure the entrypoint and command
ENTRYPOINT ["/bin/opm"]
CMD ["serve", "/configs", "--cache-dir=/tmp/cache"]
# Copy declarative config root into image at /configs and pre-populate serve cache
ADD odh-operator /configs/odh-operator
RUN ["/bin/opm", "serve", "/configs", "--cache-dir=/tmp/cache", "--cache-only"]

# Set DC-specific label for the location of the DC root directory
# in the image
LABEL operators.operatorframework.io.index.configs.v1=/configs

LABEL com.redhat.component="odh-fbc-fragment" \
      description="odh-fbc-fragment" \
      distribution-scope="public" \
      name="managed-open-data-hub/odh-fbc-fragment" \
      vendor="Red Hat, Inc." \
      summary="odh-fbc-fragment" \
      maintainer="['managed-open-data-hub@redhat.com']" \
      io.k8s.display-name="odh-fbc-fragment" \
      io.k8s.description="odh-fbc-fragment" \
      com.redhat.delivery.operator.bundle="true" \
      com.redhat.license_terms="https://www.redhat.com/licenses/Red_Hat_Standard_EULA_20191108.pdf"