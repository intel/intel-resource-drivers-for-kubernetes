module github.com/intel/intel-resource-drivers-for-kubernetes

go 1.25.0

replace github.com/intel/intel-resource-drivers-for-kubernetes/cmd/kubelet-gaudi-plugin => ./cmd/kubelet-gaudi-plugin

require (
	github.com/fsnotify/fsnotify v1.9.0
	github.com/onsi/ginkgo/v2 v2.27.2
	github.com/onsi/gomega v1.38.2
	github.com/spf13/cobra v1.10.0
	golang.org/x/sys v0.40.0
	google.golang.org/grpc v1.78.0
	gopkg.in/inf.v0 v0.9.1
	k8s.io/api v0.35.0
	k8s.io/apiextensions-apiserver v0.35.0 // indirect; to fix k8s.io/kubernetes/test/e2e/framework/pod dependency
	k8s.io/apimachinery v0.35.0
	k8s.io/client-go v0.35.0
	k8s.io/code-generator v0.35.0
	k8s.io/component-base v0.35.0
	k8s.io/controller-manager v0.35.0 // indirect; temporary
	k8s.io/dynamic-resource-allocation v0.35.0
	k8s.io/klog/v2 v2.130.1
	k8s.io/kubectl v0.35.0 // indirect; temporary
	k8s.io/kubelet v0.35.0
	k8s.io/kubernetes v1.35.0
	k8s.io/pod-security-admission v0.35.0
	tags.cncf.io/container-device-interface v1.0.1
	tags.cncf.io/container-device-interface/specs-go v1.0.0
)

require (
	cel.dev/expr v0.24.0 // indirect
	github.com/antlr4-go/antlr/v4 v4.13.0 // indirect
	github.com/beorn7/perks v1.0.1 // indirect
	github.com/blang/semver/v4 v4.0.0 // indirect
	github.com/cenkalti/backoff/v4 v4.3.0 // indirect
	github.com/cespare/xxhash/v2 v2.3.0 // indirect
	github.com/davecgh/go-spew v1.1.2-0.20180830191138-d8f796af33cc // indirect
	github.com/distribution/reference v0.6.0 // indirect
	github.com/emicklei/go-restful/v3 v3.12.2 // indirect
	github.com/felixge/httpsnoop v1.0.4 // indirect
	github.com/fxamacker/cbor/v2 v2.9.0 // indirect
	github.com/go-logr/logr v1.4.3 // indirect
	github.com/go-logr/stdr v1.2.2 // indirect
	github.com/go-openapi/jsonpointer v0.21.0 // indirect
	github.com/go-openapi/jsonreference v0.20.2 // indirect
	github.com/go-openapi/swag v0.23.0 // indirect
	github.com/go-task/slim-sprig/v3 v3.0.0 // indirect
	github.com/google/cel-go v0.26.0 // indirect
	github.com/google/gnostic-models v0.7.0 // indirect
	github.com/google/go-cmp v0.7.0 // indirect
	github.com/google/pprof v0.0.0-20250403155104-27863c87afa6 // indirect
	github.com/google/uuid v1.6.0 // indirect
	github.com/gorilla/websocket v1.5.4-0.20250319132907-e064f32e3674 // indirect
	github.com/grpc-ecosystem/grpc-gateway/v2 v2.26.3 // indirect
	github.com/inconshreveable/mousetrap v1.1.0 // indirect
	github.com/josharian/intern v1.0.0 // indirect
	github.com/json-iterator/go v1.1.12 // indirect
	github.com/mailru/easyjson v0.7.7 // indirect
	github.com/moby/spdystream v0.5.0 // indirect
	github.com/modern-go/concurrent v0.0.0-20180306012644-bacd9c7ef1dd // indirect
	github.com/modern-go/reflect2 v1.0.3-0.20250322232337-35a7c28c31ee // indirect
	github.com/munnerz/goautoneg v0.0.0-20191010083416-a7dc8b61c822 // indirect
	github.com/mxk/go-flowrate v0.0.0-20140419014527-cca7078d478f // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/runtime-spec v1.2.1 // indirect
	github.com/opencontainers/runtime-tools v0.9.1-0.20221107153022-2802ff9ff545 // indirect
	github.com/prometheus/client_golang v1.23.2 // indirect
	github.com/prometheus/client_model v0.6.2 // indirect
	github.com/prometheus/common v0.66.1 // indirect
	github.com/prometheus/procfs v0.16.1 // indirect
	github.com/spf13/pflag v1.0.9
	github.com/stoewer/go-strcase v1.3.0 // indirect
	github.com/syndtr/gocapability v0.0.0-20200815063812-42c35b437635 // indirect
	github.com/urfave/cli/v2 v2.27.5
	github.com/x448/float16 v0.8.4 // indirect
	go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp v0.61.0 // indirect
	go.opentelemetry.io/otel v1.40.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace v1.34.0 // indirect
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.34.0 // indirect
	go.opentelemetry.io/otel/metric v1.40.0 // indirect
	go.opentelemetry.io/otel/sdk v1.40.0 // indirect
	go.opentelemetry.io/otel/trace v1.40.0 // indirect
	go.opentelemetry.io/proto/otlp v1.5.0 // indirect
	go.uber.org/multierr v1.11.0 // indirect
	go.uber.org/zap v1.27.1 // indirect
	golang.org/x/exp v0.0.0-20240719175910-8a7402abbf56 // indirect
	golang.org/x/mod v0.29.0 // indirect
	golang.org/x/oauth2 v0.32.0 // indirect
	golang.org/x/sync v0.18.0 // indirect
	golang.org/x/term v0.37.0 // indirect
	golang.org/x/text v0.31.0 // indirect
	golang.org/x/time v0.9.0 // indirect
	golang.org/x/tools v0.38.0 // indirect
	google.golang.org/genproto/googleapis/api v0.0.0-20251029180050-ab9386a59fda // indirect
	google.golang.org/genproto/googleapis/rpc v0.0.0-20251222181119-0a764e51fe1b // indirect
	google.golang.org/protobuf v1.36.11 // indirect
	gopkg.in/evanphx/json-patch.v4 v4.13.0 // indirect
	gopkg.in/yaml.v3 v3.0.1 // indirect
	k8s.io/apiserver v0.35.0 // indirect
	k8s.io/component-helpers v0.35.0 // indirect
	k8s.io/gengo/v2 v2.0.0-20250922181213-ec3ebc5fd46b // indirect
	k8s.io/kube-openapi v0.0.0-20250910181357-589584f1c912 // indirect
	sigs.k8s.io/apiserver-network-proxy/konnectivity-client v0.31.2 // indirect
	sigs.k8s.io/json v0.0.0-20250730193827-2d320260d730 // indirect
	sigs.k8s.io/yaml v1.6.0 // indirect
)

require (
	github.com/cpuguy83/go-md2man/v2 v2.0.6 // indirect
	github.com/russross/blackfriday/v2 v2.1.0 // indirect
	github.com/xrash/smetrics v0.0.0-20240521201337-686a1a2994c1 // indirect
)

require (
	github.com/Masterminds/semver/v3 v3.4.0 // indirect
	github.com/go-logr/zapr v1.3.0 // indirect
	github.com/pmezard/go-difflib v1.0.1-0.20181226105442-5d4384ee4fb2 // indirect
	github.com/robfig/cron/v3 v3.0.1 // indirect
	go.etcd.io/etcd/client/pkg/v3 v3.6.5 // indirect
	go.opentelemetry.io/auto/sdk v1.2.1 // indirect
	go.yaml.in/yaml/v2 v2.4.3 // indirect
	go.yaml.in/yaml/v3 v3.0.4 // indirect
	golang.org/x/net v0.47.0 // indirect
	k8s.io/utils v0.0.0-20251002143259-bc988d571ff4 // indirect
	sigs.k8s.io/randfill v1.0.0 // indirect
	sigs.k8s.io/structured-merge-diff/v6 v6.3.0 // indirect
)
