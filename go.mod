// This is a generated file. Do not edit directly.
// Ensure you've carefully read
// https://git.k8s.io/community/contributors/devel/sig-architecture/vendor.md
// Run hack/pin-dependency.sh to change pinned dependency versions.
// Run hack/update-vendor.sh to update go.mod files and the vendor directory.

module github.com/intel/intel-resource-drivers-for-kubernetes

go 1.19

require (
	github.com/container-orchestrated-devices/container-device-interface v0.5.3
	github.com/google/uuid v1.3.0
	github.com/prometheus/client_golang v1.14.0
	github.com/spf13/cobra v1.6.0
	k8s.io/api v0.0.0
	k8s.io/apimachinery v0.24.2
	k8s.io/client-go v0.24.2
	k8s.io/code-generator v0.25.0
	k8s.io/component-base v0.24.2
	k8s.io/dynamic-resource-allocation v0.0.0
	k8s.io/klog/v2 v2.80.1
	k8s.io/kubelet v0.0.0
)

require (
	github.com/Azure/go-ansiterm v0.0.0-20210617225240-d185dfc1b5a1 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cespare/xxhash/v2 v2.1.2 // indirect
	github.com/davecgh/go-spew v1.1.1 // indirect
	github.com/emicklei/go-restful/v3 v3.9.0 // indirect
	github.com/evanphx/json-patch v4.12.0+incompatible // indirect
	github.com/fsnotify/fsnotify v1.6.0 // indirect
	github.com/go-logr/logr v1.2.3 // indirect
	github.com/go-logr/zapr v1.2.3 // indirect
	github.com/go-openapi/jsonpointer v0.19.5 // indirect
	github.com/go-openapi/jsonreference v0.20.0 // indirect
	github.com/go-openapi/swag v0.19.14 // indirect
	github.com/gogo/protobuf v1.3.2 // indirect
	github.com/golang/groupcache v0.0.0-20210331224755-41bb18bfe9da // indirect
	github.com/golang/protobuf v1.5.2 // indirect
	github.com/google/gnostic v0.5.7-v3refs // indirect
	github.com/google/go-cmp v0.5.9 // indirect
	github.com/google/gofuzz v1.1.0 // indirect
	github.com/imdario/mergo v0.3.6 // indirect
	github.com/inconshreveable/mousetrap v1.0.1 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.7.6 // indirect
	github.com/matttproud/golang_protobuf_extensions v1.0.2 // indirect
	github.com/moby/term v0.0.0-20220808134915-39b0c02b01ae // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.2 // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/opencontainers/runc v1.1.4 // indirect
	github.com/opencontainers/runtime-spec v1.0.3-0.20220825212826-86290f6a00fb // indirect
	github.com/opencontainers/runtime-tools v0.9.1-0.20221107153022-2802ff9ff545 // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/prometheus/client_model v0.3.0 // indirect
	github.com/prometheus/common v0.37.0 // indirect
	github.com/prometheus/procfs v0.8.0 // indirect
	github.com/spf13/pflag v1.0.5 // indirect
	github.com/syndtr/gocapability v0.0.0-20200815063812-42c35b437635 // indirect
	go.uber.org/atomic v1.7.0 // indirect
	go.uber.org/multierr v1.6.0 // indirect
	go.uber.org/zap v1.19.0 // indirect
	golang.org/x/mod v0.6.0 // indirect
	golang.org/x/net v0.7.0 // indirect
	golang.org/x/oauth2 v0.0.0-20220223155221-ee480838109b // indirect
	golang.org/x/sys v0.5.0 // indirect
	golang.org/x/term v0.5.0 // indirect
	golang.org/x/text v0.7.0 // indirect
	golang.org/x/time v0.0.0-20220210224613-90d013bbcef8 // indirect
	golang.org/x/tools v0.2.0 // indirect
	google.golang.org/appengine v1.6.7 // indirect
	google.golang.org/genproto v0.0.0-20220502173005-c8bf987b8c21 // indirect
	google.golang.org/grpc v1.49.0 // indirect
	google.golang.org/protobuf v1.28.1 // indirect
	gopkg.in/inf.v0 v0.9.1 // indirect
	gopkg.in/yaml.v2 v2.4.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/gengo v0.0.0-20220902162205-c0856e24416d // indirect
	k8s.io/kube-openapi v0.0.0-20221012153701-172d655c2280 // indirect
	k8s.io/utils v0.0.0-20221107191617-1a15be271d1d // indirect
	sigs.k8s.io/json v0.0.0-20220713155537-f223a00ba0e2 // indirect
	sigs.k8s.io/structured-merge-diff/v4 v4.2.3 // indirect
	sigs.k8s.io/yaml v1.3.0 // indirect
)

replace (
	k8s.io/api => github.com/kubernetes/kubernetes/staging/src/k8s.io/api v0.0.0-20221123042414-21916415cb33
	k8s.io/apiextensions-apiserver => github.com/kubernetes/kubernetes/staging/src/k8s.io/apiextensions-apiserver v0.0.0-20221123042414-21916415cb33
	k8s.io/apimachinery => github.com/kubernetes/kubernetes/staging/src/k8s.io/apimachinery v0.0.0-20221123042414-21916415cb33
	k8s.io/apiserver => github.com/kubernetes/kubernetes/staging/src/k8s.io/apiserver v0.0.0-20221123042414-21916415cb33
	k8s.io/cli-runtime => github.com/kubernetes/kubernetes/staging/src/k8s.io/cli-runtime v0.0.0-20221123042414-21916415cb33
	k8s.io/client-go => github.com/kubernetes/kubernetes/staging/src/k8s.io/client-go v0.0.0-20221123042414-21916415cb33
	k8s.io/cloud-provider => github.com/kubernetes/kubernetes/staging/src/k8s.io/cloud-provider v0.0.0-20221123042414-21916415cb33
	k8s.io/cluster-bootstrap => github.com/kubernetes/kubernetes/staging/src/k8s.io/cluster-bootstrap v0.0.0-20221123042414-21916415cb33
	k8s.io/code-generator => github.com/kubernetes/kubernetes/staging/src/k8s.io/code-generator v0.0.0-20221123042414-21916415cb33
	k8s.io/component-base => github.com/kubernetes/kubernetes/staging/src/k8s.io/component-base v0.0.0-20221123042414-21916415cb33
	k8s.io/component-helpers => github.com/kubernetes/kubernetes/staging/src/k8s.io/component-helpers v0.0.0-20221123042414-21916415cb33
	k8s.io/cri-api => github.com/kubernetes/kubernetes/staging/src/k8s.io/cri-api v0.0.0-20221123042414-21916415cb33
	k8s.io/csi-translation-lib => github.com/kubernetes/kubernetes/staging/src/k8s.io/csi-translation-lib v0.0.0-20221123042414-21916415cb33
	k8s.io/dynamic-resource-allocation => github.com/kubernetes/kubernetes/staging/src/k8s.io/dynamic-resource-allocation v0.0.0-20221123042414-21916415cb33
	k8s.io/kube-aggregator => github.com/kubernetes/kubernetes/staging/src/k8s.io/kube-aggregator v0.0.0-20221123042414-21916415cb33
	k8s.io/kube-controller-manager => github.com/kubernetes/kubernetes/staging/src/k8s.io/kube-controller-manager v0.0.0-20221123042414-21916415cb33
	k8s.io/kube-proxy => github.com/kubernetes/kubernetes/staging/src/k8s.io/kube-proxy v0.0.0-20221123042414-21916415cb33
	k8s.io/kube-scheduler => github.com/kubernetes/kubernetes/staging/src/k8s.io/kube-scheduler v0.0.0-20221123042414-21916415cb33
	k8s.io/kubectl => github.com/kubernetes/kubernetes/staging/src/k8s.io/kubectl v0.0.0-20221123042414-21916415cb33
	k8s.io/kubelet => github.com/kubernetes/kubernetes/staging/src/k8s.io/kubelet v0.0.0-20221123042414-21916415cb33
	k8s.io/kubernetes => github.com/kubernetes/kubernetes v1.27.0-alpha.0.0.20221123042414-21916415cb33
	k8s.io/legacy-cloud-providers => github.com/kubernetes/kubernetes/staging/src/k8s.io/legacy-cloud-providers v0.0.0-20221123042414-21916415cb33
	k8s.io/metrics => github.com/kubernetes/kubernetes/staging/src/k8s.io/metrics v0.0.0-20221123042414-21916415cb33
	k8s.io/sample-apiserver => github.com/kubernetes/kubernetes/staging/src/k8s.io/sample-apiserver v0.0.0-20221123042414-21916415cb33
)
