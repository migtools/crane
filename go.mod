module github.com/konveyor/crane

go 1.16

require (
	github.com/backube/pvc-transfer v0.0.0-20220627130016-a6f2935d73ac
	github.com/ghodss/yaml v1.0.0
	github.com/go-logr/logr v0.4.0
	github.com/go-logr/zapr v0.4.0
	github.com/jarcoal/httpmock v1.0.8
	github.com/konveyor/crane-lib v0.0.7
	github.com/mitchellh/mapstructure v1.4.1
	github.com/olekukonko/tablewriter v0.0.4
	github.com/openshift/api v0.0.0-20210625082935-ad54d363d274
	github.com/sirupsen/logrus v1.8.1
	github.com/spf13/cobra v1.2.1
	github.com/spf13/viper v1.8.1
	github.com/vmware-tanzu/velero v1.6.3
	go.uber.org/zap v1.19.0
	golang.org/x/mod v0.5.1
	gotest.tools v2.2.0+incompatible
	k8s.io/api v0.22.3
	k8s.io/apimachinery v0.22.3
	k8s.io/cli-runtime v0.22.2
	k8s.io/client-go v0.22.2
	sigs.k8s.io/controller-runtime v0.10.1
	sigs.k8s.io/kustomize/api v0.11.5 // indirect
	sigs.k8s.io/kustomize/cmd/config v0.10.7
	sigs.k8s.io/kustomize/kyaml v0.13.7
	sigs.k8s.io/yaml v1.3.0
)
