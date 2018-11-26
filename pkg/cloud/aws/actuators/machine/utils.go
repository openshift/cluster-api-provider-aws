/*
Copyright 2018 The Kubernetes Authors.

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

package machine

import (
	"k8s.io/apimachinery/pkg/runtime"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	providerconfigv1 "sigs.k8s.io/cluster-api-provider-aws/pkg/apis/awsproviderconfig/v1alpha1"
	awsclient "sigs.k8s.io/cluster-api-provider-aws/pkg/cloud/aws/client"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	"github.com/ghodss/yaml"
)

// ProviderConfigFromMachine gets the machine provider config MachineSetSpec from the
// specified cluster-api MachineSpec.
func ProviderConfigFromMachine(machine *clusterv1.Machine) (*providerconfigv1.AWSMachineProviderConfig, error) {
	var config providerconfigv1.AWSMachineProviderConfig
	if err := yaml.Unmarshal(machine.Spec.ProviderConfig.Value.Raw, &config); err != nil {
		return nil, err
	}
	return &config, nil
}

// ProviderStatusFromMachine gets the machine provider status from the specified machine.
func ProviderStatusFromMachine(codec codec, m *clusterv1.Machine) (*providerconfigv1.AWSMachineProviderStatus, error) {
	status := &providerconfigv1.AWSMachineProviderStatus{}
	err := codec.DecodeProviderStatus(m.Status.ProviderStatus, status)
	return status, err
}

// EncodeProviderStatus encodes the machine status into RawExtension
func EncodeProviderStatus(codec codec, awsStatus *providerconfigv1.AWSMachineProviderStatus) (*runtime.RawExtension, error) {
	return codec.EncodeProviderStatus(awsStatus)
}

// IsMaster returns true if the machine is part of a cluster's control plane
func IsMaster(machine *clusterv1.Machine) bool {
	if machineType, exists := machine.ObjectMeta.Labels[providerconfigv1.MachineTypeLabel]; exists && machineType == "master" {
		return true
	}
	return false
}

// IsInfra returns true if the machine is part of a cluster's infra plane
func IsInfra(machine *clusterv1.Machine) bool {
	if machineRole, exists := machine.ObjectMeta.Labels[providerconfigv1.MachineRoleLabel]; exists && machineRole == "infra" {
		return true
	}
	return false
}

// StringPtrsEqual safely returns true if the value for each string pointer is equal, or both are nil.
func StringPtrsEqual(s1, s2 *string) bool {
	if s1 == s2 {
		return true
	}
	if s1 == nil || s2 == nil {
		return false
	}
	return *s1 == *s2
}

// UpdateConditionCheck tests whether a condition should be updated from the
// old condition to the new condition. Returns true if the condition should
// be updated.
type UpdateConditionCheck func(oldReason, oldMessage, newReason, newMessage string) bool

// UpdateConditionAlways returns true. The condition will always be updated.
func UpdateConditionAlways(_, _, _, _ string) bool {
	return true
}

// UpdateConditionNever return false. The condition will never be updated,
// unless there is a change in the status of the condition.
func UpdateConditionNever(_, _, _, _ string) bool {
	return false
}

// UpdateConditionIfReasonOrMessageChange returns true if there is a change
// in the reason or the message of the condition.
func UpdateConditionIfReasonOrMessageChange(oldReason, oldMessage, newReason, newMessage string) bool {
	return oldReason != newReason ||
		oldMessage != newMessage
}

func shouldUpdateCondition(
	oldStatus corev1.ConditionStatus, oldReason, oldMessage string,
	newStatus corev1.ConditionStatus, newReason, newMessage string,
	updateConditionCheck UpdateConditionCheck,
) bool {
	if oldStatus != newStatus {
		return true
	}
	return updateConditionCheck(oldReason, oldMessage, newReason, newMessage)
}

// SetAWSMachineProviderCondition sets the condition for the machine and
// returns the new slice of conditions.
// If the machine does not already have a condition with the specified type,
// a condition will be added to the slice if and only if the specified
// status is True.
// If the machine does already have a condition with the specified type,
// the condition will be updated if either of the following are true.
// 1) Requested status is different than existing status.
// 2) The updateConditionCheck function returns true.
func SetAWSMachineProviderCondition(
	conditions []providerconfigv1.AWSMachineProviderCondition,
	conditionType providerconfigv1.AWSMachineProviderConditionType,
	status corev1.ConditionStatus,
	reason string,
	message string,
	updateConditionCheck UpdateConditionCheck,
) []providerconfigv1.AWSMachineProviderCondition {
	now := metav1.Now()
	existingCondition := FindAWSMachineProviderCondition(conditions, conditionType)
	if existingCondition == nil {
		if status == corev1.ConditionTrue {
			conditions = append(
				conditions,
				providerconfigv1.AWSMachineProviderCondition{
					Type:               conditionType,
					Status:             status,
					Reason:             reason,
					Message:            message,
					LastTransitionTime: now,
					LastProbeTime:      now,
				},
			)
		}
	} else {
		if shouldUpdateCondition(
			existingCondition.Status, existingCondition.Reason, existingCondition.Message,
			status, reason, message,
			updateConditionCheck,
		) {
			if existingCondition.Status != status {
				existingCondition.LastTransitionTime = now
			}
			existingCondition.Status = status
			existingCondition.Reason = reason
			existingCondition.Message = message
			existingCondition.LastProbeTime = now
		}
	}
	return conditions
}

// FindAWSMachineProviderCondition finds in the machine the condition that has the
// specified condition type. If none exists, then returns nil.
func FindAWSMachineProviderCondition(conditions []providerconfigv1.AWSMachineProviderCondition, conditionType providerconfigv1.AWSMachineProviderConditionType) *providerconfigv1.AWSMachineProviderCondition {
	for i, condition := range conditions {
		if condition.Type == conditionType {
			return &conditions[i]
		}
	}
	return nil
}

type actuatorRuntime struct {
	awsclient awsclient.Client
}

// NewClient returns default wrappers over aws services
func newActuatorRuntime(awsclient awsclient.Client) *actuatorRuntime {
	return &actuatorRuntime{
		awsclient: awsclient,
	}
}
