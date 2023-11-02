FROM scratch

# Core bundle labels.
LABEL operators.operatorframework.io.bundle.mediatype.v1=registry+v1
LABEL operators.operatorframework.io.bundle.manifests.v1=manifests/
LABEL operators.operatorframework.io.bundle.metadata.v1=metadata/
<<<<<<< HEAD
LABEL operators.operatorframework.io.bundle.package.v1=rhods-operator
LABEL operators.operatorframework.io.bundle.channels.v1=alpha,stable
LABEL operators.operatorframework.io.bundle.channel.default.v1=stable
LABEL operators.operatorframework.io.metrics.builder=operator-sdk-v1.24.1
=======
LABEL operators.operatorframework.io.bundle.package.v1=opendatahub-operator
LABEL operators.operatorframework.io.bundle.channels.v1=fast
LABEL operators.operatorframework.io.bundle.channel.default.v1=fast
<<<<<<< HEAD
LABEL operators.operatorframework.io.metrics.builder=operator-sdk-v1.32.0
>>>>>>> adb66588 (feat: introduces features fluent interface (#668))
=======
LABEL operators.operatorframework.io.metrics.builder=operator-sdk-v1.24.1
>>>>>>> 78041ea1 (fix(make): ensures projects version of operator-sdk is used (#695))
LABEL operators.operatorframework.io.metrics.mediatype.v1=metrics+v1
LABEL operators.operatorframework.io.metrics.project_layout=go.kubebuilder.io/v3

# Labels for testing.
LABEL operators.operatorframework.io.test.mediatype.v1=scorecard+v1
LABEL operators.operatorframework.io.test.config.v1=tests/scorecard/

# Copy files to locations specified by labels.
COPY bundle/manifests /manifests/
COPY bundle/metadata /metadata/
COPY bundle/tests/scorecard /tests/scorecard/
