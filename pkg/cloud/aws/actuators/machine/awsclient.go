package machine

import (
	"fmt"

	"github.com/aws/aws-sdk-go/service/ec2"

	awsclient "sigs.k8s.io/cluster-api-provider-aws/pkg/cloud/aws/client"
	clusterv1alpha1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	"github.com/openshift/cluster-api-actuator-pkg/pkg/e2e/framework"
)

type awsClientWrapper struct {
	client awsclient.Client
}

// NewAwsClientWrapper returns aws client implementaton of CloudProviderClient
// used for testing in CI environmet
func NewAwsClientWrapper(client awsclient.Client) framework.CloudProviderClient {
	return &awsClientWrapper{client: client}
}

func (client *awsClientWrapper) getNewestRunningInstance(machine *clusterv1alpha1.Machine) (*ec2.Instance, error) {
	instances, err := newActuatorRuntime(client.client).getRunningInstances(machine)
	if err != nil {
		return nil, err
	}
	sortInstances(instances)
	return instances[0], nil
}

func (client *awsClientWrapper) GetRunningInstances(machine *clusterv1alpha1.Machine) ([]interface{}, error) {
	runningInstances, err := newActuatorRuntime(client.client).getRunningInstances(machine)
	if err != nil {
		return nil, err
	}

	var instances []interface{}
	for _, instance := range runningInstances {
		instances = append(instances, instance)
	}

	return instances, nil
}

func (client *awsClientWrapper) GetPublicDNSName(machine *clusterv1alpha1.Machine) (string, error) {
	instance, err := client.getNewestRunningInstance(machine)
	if err != nil {
		return "", err
	}

	if *instance.PublicDnsName == "" {
		return "", fmt.Errorf("machine instance public DNS name not set")
	}

	return *instance.PublicDnsName, nil
}

func (client *awsClientWrapper) GetPrivateIP(machine *clusterv1alpha1.Machine) (string, error) {
	instance, err := client.getNewestRunningInstance(machine)
	if err != nil {
		return "", err
	}

	if *instance.PrivateIpAddress == "" {
		return "", fmt.Errorf("machine instance public DNS name not set")
	}

	return *instance.PrivateIpAddress, nil
}

func (client *awsClientWrapper) GetSecurityGroups(machine *clusterv1alpha1.Machine) ([]string, error) {
	instance, err := client.getNewestRunningInstance(machine)
	if err != nil {
		return nil, err
	}
	var groups []string
	for _, groupIdentifier := range instance.SecurityGroups {
		if *groupIdentifier.GroupName != "" {
			groups = append(groups, *groupIdentifier.GroupName)
		}
	}
	return groups, nil
}

func (client *awsClientWrapper) GetIAMRole(machine *clusterv1alpha1.Machine) (string, error) {
	instance, err := client.getNewestRunningInstance(machine)
	if err != nil {
		return "", err
	}
	if instance.IamInstanceProfile == nil {
		return "", err
	}
	return *instance.IamInstanceProfile.Id, nil
}

func (client *awsClientWrapper) GetTags(machine *clusterv1alpha1.Machine) (map[string]string, error) {
	instance, err := client.getNewestRunningInstance(machine)
	if err != nil {
		return nil, err
	}
	tags := make(map[string]string, len(instance.Tags))
	for _, tag := range instance.Tags {
		tags[*tag.Key] = *tag.Value
	}
	return tags, nil
}

func (client *awsClientWrapper) GetSubnet(machine *clusterv1alpha1.Machine) (string, error) {
	instance, err := client.getNewestRunningInstance(machine)
	if err != nil {
		return "", err
	}
	if instance.SubnetId == nil {
		return "", err
	}
	return *instance.SubnetId, nil
}

func (client *awsClientWrapper) GetAvailabilityZone(machine *clusterv1alpha1.Machine) (string, error) {
	instance, err := client.getNewestRunningInstance(machine)
	if err != nil {
		return "", err
	}
	if instance.Placement == nil {
		return "", err
	}
	return *instance.Placement.AvailabilityZone, nil
}
