module sigs.k8s.io/cluster-api-provider-aws

go 1.15

require (
	github.com/aws/aws-sdk-go v1.38.25
	github.com/blang/semver v3.5.1+incompatible
	github.com/go-logr/logr v0.3.0
	github.com/golang/mock v1.4.4
	github.com/onsi/ginkgo v1.14.1
	github.com/onsi/gomega v1.10.2
	github.com/openshift/api v0.0.0-20210428205234-a8389931bee7
	github.com/openshift/machine-api-operator v0.2.1-0.20210516083017-bb9e0b5c1170

	// k8s
	k8s.io/api v0.20.6
	k8s.io/apimachinery v0.20.6
	k8s.io/client-go v0.20.6
	k8s.io/klog/v2 v2.4.0
	k8s.io/utils v0.0.0-20201110183641-67b214c5f920
	sigs.k8s.io/controller-runtime v0.7.0
	sigs.k8s.io/controller-tools v0.3.0
	sigs.k8s.io/yaml v1.2.0
)

replace (
	sigs.k8s.io/cluster-api-provider-aws v0.0.0-00010101000000-000000000000 => github.com/openshift/cluster-api-provider-aws v0.2.1-0.20210430231032-3967c2861801
	sigs.k8s.io/cluster-api-provider-azure v0.0.0-00010101000000-000000000000 => github.com/openshift/cluster-api-provider-azure v0.1.0-alpha.3.0.20210318155632-e744815d9f05
)
