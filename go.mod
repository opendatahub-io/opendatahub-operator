module github.com/opendatahub-io/opendatahub-operator/v2

go 1.20

require (
	github.com/blang/semver/v4 v4.0.0
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v1.4.1
	github.com/hashicorp/go-multierror v1.1.1
	github.com/onsi/ginkgo/v2 v2.12.1
	github.com/onsi/gomega v1.27.10
	github.com/opendatahub-io/opendatahub-operator v1.7.0
	github.com/openshift/addon-operator/apis v0.0.0-20230919043633-820afed15881
	github.com/openshift/api v0.0.0-20230823114715-5fdd7511b790
	github.com/openshift/custom-resource-status v1.1.2
	github.com/operator-framework/api v0.18.0
	github.com/pkg/errors v0.9.1
	github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring v0.68.0
	github.com/spf13/afero v1.10.0
	github.com/stretchr/testify v1.8.4
	go.uber.org/zap v1.26.0
	golang.org/x/exp v0.0.0-20231006140011-7918f672742d
	gopkg.in/yaml.v2 v2.4.0
	k8s.io/api v0.28.3
	k8s.io/apiextensions-apiserver v0.28.2
	k8s.io/apimachinery v0.28.3
	k8s.io/client-go v11.0.0+incompatible
	k8s.io/kube-aggregator v0.28.3
	sigs.k8s.io/controller-runtime v0.16.1
	sigs.k8s.io/kustomize/api v0.13.4
	sigs.k8s.io/kustomize/kyaml v0.14.2
)

require (
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/cespare/xxhash/v2 v2.2.0 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/emicklei/go-restful/v3 v3.10.2 // indirect
	github.com/evanphx/json-patch v5.6.0+incompatible // indirect
	github.com/evanphx/json-patch/v5 v5.6.0 // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-errors/errors v1.4.2 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/go-openapi/jsonpointer v0.19.6 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.22.3 // indirect
	github.com/go-task/slim-sprig v0.0.0-20230315185526-52ccab3ef572 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.4 // indirect
	github.com/google/gnostic v0.6.9 // indirect
	github.com/google/go-cmp v0.6.0 // indirect
	github.com/google/gofuzz v1.2.0 // indirect
	github.com/google/pprof v0.0.0-20221118152302-e6195bd50e26 // indirect
	github.com/google/shlex v0.0.0-20191202100458-e7afc7fbc510 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/imdario/mergo v0.3.13 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.4 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/monochromegane/go-gitignore v0.0.0-20200626010858-205db1a8cc00 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/pmezard/go-difflib v1.0.0 // indirect
	github.com/prometheus/client_golang v1.16.0 // indirect
	github.com/prometheus/client_model v0.5.0 // indirect
	github.com/prometheus/common v0.44.0 // indirect
	github.com/prometheus/procfs v0.10.1 // indirect
	github.com/rhobs/obo-prometheus-operator/pkg/apis/monitoring v0.61.1-rhobs1 // indirect
	github.com/rogpeppe/go-internal v1.11.0 // indirect
	github.com/sergi/go-diff v1.2.0 // indirect
	github.com/sirupsen/logrus v1.9.2 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/xlab/treeprint v1.2.0 // indirect
	go.starlark.net v0.0.0-20200306205701-8dd3e2ee1dd5 // indirect
	go.uber.org/atomic v1.10.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	golang.org/x/net v0.23.0 // indirect
	golang.org/x/oauth2 v0.17.0 // indirect
	golang.org/x/sys v0.18.0 // indirect
	golang.org/x/term v0.18.0 // indirect
	golang.org/x/text v0.14.0 // indirect
	golang.org/x/time v0.5.0 // indirect
	golang.org/x/tools v0.14.0 // indirect
	gomodules.xyz/jsonpatch/v2 v2.4.0 // indirect
	google.golang.org/appengine v1.6.8 // indirect
	google.golang.org/protobuf v1.33.0 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/component-base v0.28.3 // indirect
	k8s.io/klog/v2 v2.100.1 // indirect
	k8s.io/kube-openapi v0.0.0-20230717233707-2695361300d9 // indirect
	k8s.io/utils v0.0.0-20230726121419-3b25d923346b // indirect
	sigs.k8s.io/json v0.0.0-20221116044647-bc3834ca7abd // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.3.0 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)

replace (
	github.com/go-yaml/yaml => github.com/go-yaml/yaml v2.2.8+incompatible
	github.com/mattn/go-sqlite3 => github.com/mattn/go-sqlite3 v1.14.18
	github.com/tektoncd/pipeline => github.com/tektoncd/pipeline v0.12.0
	go.uber.org/multierr => go.uber.org/multierr v1.6.0
	google.golang.org/grpc => google.golang.org/grpc v1.63.2
	k8s.io/api => k8s.io/api v0.26.0
	k8s.io/client-go => k8s.io/client-go v0.26.0
	k8s.io/kube-openapi => k8s.io/kube-openapi v0.0.0-20230113010536-ace3e4a52ca1
	sigs.k8s.io/controller-runtime => sigs.k8s.io/controller-runtime v0.14.6
)

exclude github.com/openshift/api v3.9.0+incompatible
