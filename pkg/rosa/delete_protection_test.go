/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package rosa_test

import (
	"testing"

	"github.com/golang/mock/gomock"
	. "github.com/onsi/gomega"
	cmv1 "github.com/openshift-online/ocm-sdk-go/clustersmgmt/v1"

	rosacontrolplanev1 "sigs.k8s.io/cluster-api-provider-aws/v2/controlplane/rosa/api/v1beta2"
	"sigs.k8s.io/cluster-api-provider-aws/v2/pkg/cloud/scope"
	"sigs.k8s.io/cluster-api-provider-aws/v2/pkg/rosa"
	"sigs.k8s.io/cluster-api-provider-aws/v2/test/mocks"
)

func TestDeleteProtectionEnabledFromCluster(t *testing.T) {
	g := NewWithT(t)

	t.Run("returns false when cluster is nil", func(t *testing.T) {
		enabled, ok := rosa.DeleteProtectionEnabledFromCluster(nil)
		g.Expect(ok).To(BeFalse())
		g.Expect(enabled).To(BeFalse())
	})

	t.Run("returns enabled value when inline field is present", func(t *testing.T) {
		cluster, err := cmv1.NewCluster().
			DeleteProtection(cmv1.NewDeleteProtection().Enabled(true)).
			Build()
		g.Expect(err).NotTo(HaveOccurred())

		enabled, ok := rosa.DeleteProtectionEnabledFromCluster(cluster)
		g.Expect(ok).To(BeTrue())
		g.Expect(enabled).To(BeTrue())
	})

	t.Run("returns false when inline field reports disabled", func(t *testing.T) {
		cluster, err := cmv1.NewCluster().
			DeleteProtection(cmv1.NewDeleteProtection().Enabled(false)).
			Build()
		g.Expect(err).NotTo(HaveOccurred())

		enabled, ok := rosa.DeleteProtectionEnabledFromCluster(cluster)
		g.Expect(ok).To(BeTrue())
		g.Expect(enabled).To(BeFalse())
	})
}

func TestReconcileDeleteProtection(t *testing.T) {
	g := NewWithT(t)

	cluster, err := cmv1.NewCluster().
		ID("cluster-1").
		DeleteProtection(cmv1.NewDeleteProtection().Enabled(false)).
		Build()
	g.Expect(err).NotTo(HaveOccurred())

	rosaControlPlane := &rosacontrolplanev1.ROSAControlPlane{
		Spec: rosacontrolplanev1.RosaControlPlaneSpec{
			DeleteProtection: rosacontrolplanev1.DeleteProtectionEnabled,
		},
	}

	mockCtrl := gomock.NewController(t)
	ocmMock := mocks.NewMockOCMClient(mockCtrl)
	ocmMock.EXPECT().
		UpdateClusterDeletionProtection("cluster-1", true).
		Return(nil).
		Times(1)

	rosaScope := &scope.ROSAControlPlaneScope{
		ControlPlane: rosaControlPlane,
	}

	g.Expect(rosa.ReconcileDeleteProtection(rosaScope, ocmMock, cluster)).To(Succeed())
}

func TestIsDeleteProtectionBlocking(t *testing.T) {
	g := NewWithT(t)

	cluster, err := cmv1.NewCluster().
		ID("cluster-1").
		DeleteProtection(cmv1.NewDeleteProtection().Enabled(true)).
		Build()
	g.Expect(err).NotTo(HaveOccurred())

	t.Run("blocks when spec requests protection", func(t *testing.T) {
		rosaScope := &scope.ROSAControlPlaneScope{
			ControlPlane: &rosacontrolplanev1.ROSAControlPlane{
				Spec: rosacontrolplanev1.RosaControlPlaneSpec{
					DeleteProtection: rosacontrolplanev1.DeleteProtectionEnabled,
				},
			},
		}

		blocking := rosa.IsDeleteProtectionBlocking(rosaScope, cluster)
		g.Expect(blocking).To(BeTrue())
	})

	t.Run("blocks when live protection is enabled", func(t *testing.T) {
		rosaScope := &scope.ROSAControlPlaneScope{
			ControlPlane: &rosacontrolplanev1.ROSAControlPlane{
				Spec: rosacontrolplanev1.RosaControlPlaneSpec{
					DeleteProtection: rosacontrolplanev1.DeleteProtectionDisabled,
				},
			},
		}

		blocking := rosa.IsDeleteProtectionBlocking(rosaScope, cluster)
		g.Expect(blocking).To(BeTrue())
	})

	t.Run("allows delete when protection is disabled", func(t *testing.T) {
		disabledCluster, err := cmv1.NewCluster().
			ID("cluster-1").
			DeleteProtection(cmv1.NewDeleteProtection().Enabled(false)).
			Build()
		g.Expect(err).NotTo(HaveOccurred())

		rosaScope := &scope.ROSAControlPlaneScope{
			ControlPlane: &rosacontrolplanev1.ROSAControlPlane{
				Spec: rosacontrolplanev1.RosaControlPlaneSpec{
					DeleteProtection: rosacontrolplanev1.DeleteProtectionDisabled,
				},
			},
		}

		blocking := rosa.IsDeleteProtectionBlocking(rosaScope, disabledCluster)
		g.Expect(blocking).To(BeFalse())
	})
}
