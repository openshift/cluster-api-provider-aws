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
	"encoding/base64"
	"fmt"
	"time"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"

	providerconfigv1 "sigs.k8s.io/cluster-api-provider-aws/pkg/cloud/aws/providerconfig/v1alpha1"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterclient "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
	clustererror "sigs.k8s.io/cluster-api/pkg/controller/error"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"

	awsclient "sigs.k8s.io/cluster-api-provider-aws/pkg/cloud/aws/client"
	clustoplog "sigs.k8s.io/cluster-api-provider-aws/pkg/cloud/aws/logging"
)

const (
	userDataSecretKey         = "userData"
	ec2InstanceIDNotFoundCode = "InvalidInstanceID.NotFound"
	requeueAfterSeconds       = 20

	// MachineCreationSucceeded indicates success for machine creation
	MachineCreationSucceeded = "MachineCreationSucceeded"

	// MachineCreationFailed indicates that machine creation failed
	MachineCreationFailed = "MachineCreationFailed"
)

// Actuator is the AWS-specific actuator for the Cluster API machine controller
type Actuator struct {
	kubeClient       kubernetes.Interface
	clusterClient    clusterclient.Interface
	logger           *log.Entry
	awsClientBuilder awsclient.AwsClientBuilderFuncType
}

// ActuatorParams holds parameter information for Actuator
type ActuatorParams struct {
	KubeClient       kubernetes.Interface
	ClusterClient    clusterclient.Interface
	Logger           *log.Entry
	AwsClientBuilder awsclient.AwsClientBuilderFuncType
}

// NewActuator returns a new AWS Actuator
func NewActuator(params ActuatorParams) (*Actuator, error) {
	actuator := &Actuator{
		kubeClient:       params.KubeClient,
		clusterClient:    params.ClusterClient,
		logger:           params.Logger,
		awsClientBuilder: params.AwsClientBuilder,
	}
	return actuator, nil
}

// Create runs a new EC2 instance
func (a *Actuator) Create(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	mLog := clustoplog.WithMachine(a.logger, machine)
	mLog.Info("creating machine")
	instance, err := a.CreateMachine(cluster, machine)
	if err != nil {
		mLog.Errorf("error creating machine: %v", err)
		updateConditionError := a.updateMachineProviderConditions(machine, mLog, providerconfigv1.MachineCreation, MachineCreationFailed, err.Error())
		if updateConditionError != nil {
			mLog.Errorf("error updating machine conditions: %v", updateConditionError)
		}
		return err
	}

	// TODO(csrwng):
	// Part of the status that gets updated when the machine gets created is the PublicIP.
	// However, after a call to runInstance, most of the time a PublicIP is not yet allocated.
	// If we don't yet have complete status (ie. the instance state is Pending, instead of Running),
	// maybe we should return an error so the machine controller keeps retrying until we have complete status we can set.
	return a.updateStatus(machine, instance, mLog)
}

func (a *Actuator) updateMachineStatus(machine *clusterv1.Machine, awsStatus *providerconfigv1.AWSMachineProviderStatus, mLog log.FieldLogger, networkAddresses []corev1.NodeAddress) error {
	awsStatusRaw, err := EncodeAWSMachineProviderStatus(awsStatus)
	if err != nil {
		mLog.Errorf("error encoding AWS provider status: %v", err)
		return err
	}

	machineCopy := machine.DeepCopy()
	machineCopy.Status.ProviderStatus = awsStatusRaw
	if networkAddresses != nil {
		machineCopy.Status.Addresses = networkAddresses
	}

	if !equality.Semantic.DeepEqual(machine.Status, machineCopy.Status) {
		mLog.Info("machine status has changed, updating")
		machineCopy.Status.LastUpdated = metav1.Now()

		_, err := a.clusterClient.ClusterV1alpha1().Machines(machineCopy.Namespace).UpdateStatus(machineCopy)
		if err != nil {
			mLog.Errorf("error updating machine status: %v", err)
			return err
		}
	} else {
		mLog.Debug("status unchanged")
	}

	return nil
}

// updateMachineProviderConditions updates conditions set within machine provider status.
func (a *Actuator) updateMachineProviderConditions(machine *clusterv1.Machine, mLog log.FieldLogger, conditionType providerconfigv1.AWSMachineProviderConditionType, reason string, msg string) error {

	mLog.Debug("updating machine conditions")

	awsStatus, err := AWSMachineProviderStatusFromClusterAPIMachine(machine)
	if err != nil {
		mLog.Errorf("error decoding machine provider status: %v", err)
		return err
	}

	awsStatus.Conditions = SetAWSMachineProviderCondition(awsStatus.Conditions, conditionType, corev1.ConditionTrue, reason, msg, UpdateConditionIfReasonOrMessageChange)

	err = a.updateMachineStatus(machine, awsStatus, mLog, nil)
	if err != nil {
		return err
	}

	return nil
}

// removeStoppedMachine removes all instances of a specific machine that are in a stopped state.
func (a *Actuator) removeStoppedMachine(machine *clusterv1.Machine, client awsclient.Client, mLog log.FieldLogger) error {
	instances, err := GetStoppedInstances(machine, client)
	if err != nil {
		mLog.Errorf("error getting stopped instances: %v", err)
		return fmt.Errorf("error getting stopped instances: %v", err)
	}

	if len(instances) == 0 {
		mLog.Infof("no stopped instances found for machine %v", machine.Name)
		return nil
	}

	return TerminateInstances(client, instances, mLog)
}

func buildEc2Filters(inputFilters []providerconfigv1.Filter) []*ec2.Filter {
	filters := make([]*ec2.Filter, len(inputFilters))
	for i, f := range inputFilters {
		values := make([]*string, len(f.Values))
		for j, v := range f.Values {
			values[j] = aws.String(v)
		}
		filters[i] = &ec2.Filter{
			Name:   aws.String(f.Name),
			Values: values,
		}
	}
	return filters
}

func getSecurityGroupsIDs(securityGroups []providerconfigv1.AWSResourceReference, client awsclient.Client, mLog log.FieldLogger) ([]*string, error) {
	var securityGroupIDs []*string
	for _, g := range securityGroups {
		// ID has priority
		if g.ID != nil {
			securityGroupIDs = append(securityGroupIDs, g.ID)
		} else if g.Filters != nil {
			mLog.Debug("Describing security groups based on filters")
			// Get groups based on filters
			describeSecurityGroupsRequest := ec2.DescribeSecurityGroupsInput{
				Filters: buildEc2Filters(g.Filters),
			}
			describeSecurityGroupsResult, err := client.DescribeSecurityGroups(&describeSecurityGroupsRequest)
			if err != nil {
				mLog.Errorf("error describing security groups: %v", err)
				return nil, fmt.Errorf("error describing security groups: %v", err)
			}
			for _, g := range describeSecurityGroupsResult.SecurityGroups {
				groupID := *g.GroupId
				securityGroupIDs = append(securityGroupIDs, &groupID)
			}
		}
	}

	if len(securityGroups) == 0 {
		mLog.Debug("No security group found")
	}

	return securityGroupIDs, nil
}

func getSubnetIDs(subnet providerconfigv1.AWSResourceReference, client awsclient.Client, mLog log.FieldLogger) ([]*string, error) {
	var subnetIDs []*string
	// ID has priority
	if subnet.ID != nil {
		subnetIDs = append(subnetIDs, subnet.ID)
	} else {
		mLog.Debug("Describing subnets based on filters")
		describeSubnetRequest := ec2.DescribeSubnetsInput{
			Filters: buildEc2Filters(subnet.Filters),
		}
		describeSubnetResult, err := client.DescribeSubnets(&describeSubnetRequest)
		if err != nil {
			mLog.Errorf("error describing security groups: %v", err)
			return nil, fmt.Errorf("error describing security groups: %v", err)
		}
		for _, n := range describeSubnetResult.Subnets {
			subnetID := *n.SubnetId
			subnetIDs = append(subnetIDs, &subnetID)
		}
	}
	if len(subnetIDs) == 0 {
		return nil, fmt.Errorf("no subnet IDs were found")
	}
	return subnetIDs, nil
}

// CreateMachine starts a new AWS instance as described by the cluster and machine resources
func (a *Actuator) CreateMachine(cluster *clusterv1.Cluster, machine *clusterv1.Machine) (*ec2.Instance, error) {
	mLog := clustoplog.WithMachine(a.logger, machine)

	machineProviderConfig, err := ProviderConfigFromClusterAPIMachineSpec(&machine.Spec)
	if err != nil {
		mLog.Errorf("error decoding MachineProviderConfig: %v", err)
		return nil, err
	}

	credentialsSecretName := ""
	if machineProviderConfig.CredentialsSecret != nil {
		credentialsSecretName = machineProviderConfig.CredentialsSecret.Name
	}
	client, err := a.awsClientBuilder(a.kubeClient, credentialsSecretName, machine.Namespace, machineProviderConfig.Placement.Region)
	if err != nil {
		mLog.Errorf("unable to obtain AWS client: %v", err)
		return nil, fmt.Errorf("unable to obtain AWS client: %v", err)
	}

	// We explicitly do NOT want to remove stopped masters.
	if !IsMaster(machine) {
		// Prevent having a lot of stopped nodes sitting around.
		err = a.removeStoppedMachine(machine, client, mLog)
		if err != nil {
			mLog.Errorf("unable to remove stopped machines: %v", err)
			return nil, fmt.Errorf("unable to remove stopped nodes: %v", err)
		}
	}

	// Get AMI to use
	var amiID *string
	if machineProviderConfig.AMI.ID != nil {
		amiID = machineProviderConfig.AMI.ID
		mLog.Debugf("Using AMI %s", *amiID)
	} else if len(machineProviderConfig.AMI.Filters) > 0 {
		mLog.Debug("Describing AMI based on filters")
		describeImagesRequest := ec2.DescribeImagesInput{
			Filters: buildEc2Filters(machineProviderConfig.AMI.Filters),
		}
		describeAMIResult, err := client.DescribeImages(&describeImagesRequest)
		if err != nil {
			mLog.Errorf("error describing AMI: %v", err)
			return nil, fmt.Errorf("error describing AMI: %v", err)
		}
		if len(describeAMIResult.Images) < 1 {
			mLog.Errorf("no image for given filters not found")
			return nil, fmt.Errorf("no image for given filters not found")
		}
		latestImage := describeAMIResult.Images[0]
		latestTime, err := time.Parse(time.RFC3339, *latestImage.CreationDate)
		if err != nil {
			mLog.Errorf("unable to parse time for %q AMI: %v", *latestImage.ImageId, err)
			return nil, fmt.Errorf("unable to parse time for %q AMI: %v", *latestImage.ImageId, err)
		}
		for _, image := range describeAMIResult.Images[1:] {
			imageTime, err := time.Parse(time.RFC3339, *image.CreationDate)
			if err != nil {
				mLog.Errorf("unable to parse time for %q AMI: %v", *image.ImageId, err)
				return nil, fmt.Errorf("unable to parse time for %q AMI: %v", *image.ImageId, err)
			}
			if latestTime.Before(imageTime) {
				latestImage = image
				latestTime = imageTime
			}
		}
		amiID = latestImage.ImageId
	} else {
		return nil, fmt.Errorf("AMI ID or AMI filters need to be specified")
	}

	securityGroupsIDs, err := getSecurityGroupsIDs(machineProviderConfig.SecurityGroups, client, mLog)
	if err != nil {
		return nil, fmt.Errorf("error getting security groups IDs: %v,", err)
	}
	subnetIDs, err := getSubnetIDs(machineProviderConfig.Subnet, client, mLog)
	if err != nil {
		return nil, fmt.Errorf("error getting subnet IDs: %v,", err)
	}
	if len(subnetIDs) > 1 {
		mLog.Warnf("More than one subnet id returned, only first one will be used")
	}

	// build list of networkInterfaces (just 1 for now)
	var networkInterfaces = []*ec2.InstanceNetworkInterfaceSpecification{
		{
			DeviceIndex:              aws.Int64(machineProviderConfig.DeviceIndex),
			AssociatePublicIpAddress: machineProviderConfig.PublicIP,
			SubnetId:                 subnetIDs[0],
			Groups:                   securityGroupsIDs,
		},
	}

	clusterID, ok := getClusterID(machine)
	if !ok {
		mLog.Errorf("unable to get cluster ID for machine: %q", machine.Name)
		return nil, err
	}
	// Add tags to the created machine
	tagList := []*ec2.Tag{}
	for _, tag := range machineProviderConfig.Tags {
		tagList = append(tagList, &ec2.Tag{Key: aws.String(tag.Name), Value: aws.String(tag.Value)})
	}
	tagList = append(tagList, []*ec2.Tag{
		{Key: aws.String("clusterid"), Value: aws.String(clusterID)},
		{Key: aws.String("kubernetes.io/cluster/" + clusterID), Value: aws.String("owned")},
		{Key: aws.String("Name"), Value: aws.String(machine.Name)},
	}...)

	tagInstance := &ec2.TagSpecification{
		ResourceType: aws.String("instance"),
		Tags:         tagList,
	}
	tagVolume := &ec2.TagSpecification{
		ResourceType: aws.String("volume"),
		Tags:         []*ec2.Tag{{Key: aws.String("clusterid"), Value: aws.String(clusterID)}},
	}

	userData := []byte{}
	if machineProviderConfig.UserDataSecret != nil {
		userDataSecret, err := a.kubeClient.CoreV1().Secrets(machine.Namespace).Get(machineProviderConfig.UserDataSecret.Name, metav1.GetOptions{})
		if err != nil {
			mLog.Errorf("error getting user data secret %s: %v", machineProviderConfig.UserDataSecret.Name, err)
			return nil, err
		}
		if data, exists := userDataSecret.Data[userDataSecretKey]; exists {
			userData = data
		} else {
			glog.Warningf("Secret %v/%v does not have %q field set. Thus, no user data applied when creating an instance.", machine.Namespace, machineProviderConfig.UserDataSecret.Name, userDataSecretKey)
		}
	}

	userDataEnc := base64.StdEncoding.EncodeToString(userData)

	var iamInstanceProfile *ec2.IamInstanceProfileSpecification
	if machineProviderConfig.IAMInstanceProfile != nil && machineProviderConfig.IAMInstanceProfile.ID != nil {
		iamInstanceProfile = &ec2.IamInstanceProfileSpecification{
			Name: aws.String(*machineProviderConfig.IAMInstanceProfile.ID),
		}
	}

	inputConfig := ec2.RunInstancesInput{
		ImageId:      amiID,
		InstanceType: aws.String(machineProviderConfig.InstanceType),
		// Only a single instance of the AWS instance allowed
		MinCount:           aws.Int64(1),
		MaxCount:           aws.Int64(1),
		KeyName:            machineProviderConfig.KeyName,
		IamInstanceProfile: iamInstanceProfile,
		TagSpecifications:  []*ec2.TagSpecification{tagInstance, tagVolume},
		NetworkInterfaces:  networkInterfaces,
		UserData:           &userDataEnc,
	}

	runResult, err := client.RunInstances(&inputConfig)
	if err != nil {
		mLog.Errorf("error creating EC2 instance: %v", err)
		return nil, fmt.Errorf("error creating EC2 instance: %v", err)
	}

	if runResult == nil || len(runResult.Instances) != 1 {
		mLog.Errorf("unexpected reservation creating instances: %v", runResult)
		return nil, fmt.Errorf("unexpected reservation creating instance")
	}

	// TOOD(csrwng):
	// One issue we have right now with how this works, is that usually at the end of the RunInstances call,
	// the instance state is not yet 'Running'. Rather it is 'Pending'. Therefore the status
	// is not yet complete (like PublicIP). One possible fix would be to wait and poll here
	// until the instance is in the Running state. The other approach is to return an error
	// so that the machine is requeued and in the exists function return false if the status doesn't match.
	// That would require making the create re-entrant so we can just update the status.
	return runResult.Instances[0], nil
}

// Delete deletes a machine and updates its finalizer
func (a *Actuator) Delete(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	mLog := clustoplog.WithMachine(a.logger, machine)
	mLog.Info("deleting machine")
	if err := a.DeleteMachine(cluster, machine); err != nil {
		mLog.Errorf("error deleting machine: %v", err)
		return err
	}
	return nil
}

// DeleteMachine deletes an AWS instance
func (a *Actuator) DeleteMachine(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	mLog := clustoplog.WithMachine(a.logger, machine)

	machineProviderConfig, err := ProviderConfigFromClusterAPIMachineSpec(&machine.Spec)
	if err != nil {
		mLog.Errorf("error decoding MachineProviderConfig: %v", err)
		return err
	}

	region := machineProviderConfig.Placement.Region
	credentialsSecretName := ""
	if machineProviderConfig.CredentialsSecret != nil {
		credentialsSecretName = machineProviderConfig.CredentialsSecret.Name
	}
	client, err := a.awsClientBuilder(a.kubeClient, credentialsSecretName, machine.Namespace, region)
	if err != nil {
		mLog.Errorf("error getting EC2 client: %v", err)
		return fmt.Errorf("error getting EC2 client: %v", err)
	}

	instances, err := GetRunningInstances(machine, client)
	if err != nil {
		mLog.Errorf("error getting running instances: %v", err)
		return err
	}
	if len(instances) == 0 {
		mLog.Warnf("no instances found to delete for machine")
		return nil
	}

	return TerminateInstances(client, instances, mLog)
}

// Update attempts to sync machine state with an existing instance. Today this just updates status
// for details that may have changed. (IPs and hostnames) We do not currently support making any
// changes to actual machines in AWS. Instead these will be replaced via MachineDeployments.
func (a *Actuator) Update(cluster *clusterv1.Cluster, machine *clusterv1.Machine) error {
	mLog := clustoplog.WithMachine(a.logger, machine)
	mLog.Debugf("updating machine")

	machineProviderConfig, err := ProviderConfigFromClusterAPIMachineSpec(&machine.Spec)
	if err != nil {
		mLog.Errorf("error decoding MachineProviderConfig: %v", err)
		return err
	}

	region := machineProviderConfig.Placement.Region
	mLog.WithField("region", region).Debugf("obtaining EC2 client for region")
	credentialsSecretName := ""
	if machineProviderConfig.CredentialsSecret != nil {
		credentialsSecretName = machineProviderConfig.CredentialsSecret.Name
	}
	client, err := a.awsClientBuilder(a.kubeClient, credentialsSecretName, machine.Namespace, region)
	if err != nil {
		mLog.Errorf("error getting EC2 client: %v", err)
		return fmt.Errorf("unable to obtain EC2 client: %v", err)
	}

	instances, err := GetRunningInstances(machine, client)
	if err != nil {
		mLog.Errorf("error getting running instances: %v", err)
		return err
	}
	mLog.Debugf("found %d instances for machine", len(instances))

	// Parent controller should prevent this from ever happening by calling Exists and then Create,
	// but instance could be deleted between the two calls.
	if len(instances) == 0 {
		mLog.Warnf("attempted to update machine but no instances found")
		// Update status to clear out machine details.
		err := a.updateStatus(machine, nil, mLog)
		if err != nil {
			return err
		}
		mLog.Errorf("attempted to update machine but no instances found")
		return fmt.Errorf("attempted to update machine but no instances found")
	}
	newestInstance, terminateInstances := SortInstances(instances)

	// In very unusual circumstances, there could be more than one machine running matching this
	// machine name and cluster ID. In this scenario we will keep the newest, and delete all others.
	mLog = mLog.WithField("instanceID", *newestInstance.InstanceId)
	mLog.Debug("instance found")

	if len(instances) > 1 {
		err = TerminateInstances(client, terminateInstances, mLog)
		if err != nil {
			return err
		}

	}

	// We do not support making changes to pre-existing instances, just update status.
	return a.updateStatus(machine, newestInstance, mLog)
}

// Exists determines if the given machine currently exists. For AWS we query for instances in
// running state, with a matching name tag, to determine a match.
func (a *Actuator) Exists(cluster *clusterv1.Cluster, machine *clusterv1.Machine) (bool, error) {
	mLog := clustoplog.WithMachine(a.logger, machine)
	mLog.Debugf("checking if machine exists")

	instances, err := a.getMachineInstances(cluster, machine)
	if err != nil {
		mLog.Errorf("error getting running instances: %v", err)
		return false, err
	}
	if len(instances) == 0 {
		mLog.Debug("instance does not exist")
		return false, nil
	}

	// If more than one result was returned, it will be handled in Update.
	mLog.Debugf("instance exists as %q", *instances[0].InstanceId)
	return true, nil
}

// Describe provides information about machine's instance(s)
func (a *Actuator) Describe(cluster *clusterv1.Cluster, machine *clusterv1.Machine) (*ec2.Instance, error) {
	mLog := clustoplog.WithMachine(a.logger, machine)
	mLog.Debugf("checking if machine exists")

	instances, err := a.getMachineInstances(cluster, machine)
	if err != nil {
		mLog.Errorf("error getting running instances: %v", err)
		return nil, err
	}
	if len(instances) == 0 {
		mLog.Debug("instance does not exist")
		return nil, nil
	}

	return instances[0], nil
}

func (a *Actuator) getMachineInstances(cluster *clusterv1.Cluster, machine *clusterv1.Machine) ([]*ec2.Instance, error) {
	mLog := clustoplog.WithMachine(a.logger, machine)

	machineProviderConfig, err := ProviderConfigFromClusterAPIMachineSpec(&machine.Spec)
	if err != nil {
		mLog.Errorf("error decoding MachineProviderConfig: %v", err)
		return nil, err
	}

	region := machineProviderConfig.Placement.Region
	credentialsSecretName := ""
	if machineProviderConfig.CredentialsSecret != nil {
		credentialsSecretName = machineProviderConfig.CredentialsSecret.Name
	}
	client, err := a.awsClientBuilder(a.kubeClient, credentialsSecretName, machine.Namespace, region)
	if err != nil {
		mLog.Errorf("error getting EC2 client: %v", err)
		return nil, fmt.Errorf("error getting EC2 client: %v", err)
	}

	return GetRunningInstances(machine, client)
}

// updateStatus calculates the new machine status, checks if anything has changed, and updates if so.
func (a *Actuator) updateStatus(machine *clusterv1.Machine, instance *ec2.Instance, mLog log.FieldLogger) error {

	mLog.Debug("updating status")

	// Starting with a fresh status as we assume full control of it here.
	awsStatus, err := AWSMachineProviderStatusFromClusterAPIMachine(machine)
	if err != nil {
		mLog.Errorf("error decoding machine provider status: %v", err)
		return err
	}
	// Save this, we need to check if it changed later.
	networkAddresses := []corev1.NodeAddress{}

	// Instance may have existed but been deleted outside our control, clear it's status if so:
	if instance == nil {
		awsStatus.InstanceID = nil
		awsStatus.InstanceState = nil
	} else {
		awsStatus.InstanceID = instance.InstanceId
		awsStatus.InstanceState = instance.State.Name
		if instance.PublicIpAddress != nil {
			networkAddresses = append(networkAddresses, corev1.NodeAddress{
				Type:    corev1.NodeExternalIP,
				Address: *instance.PublicIpAddress,
			})
		}
		if instance.PrivateIpAddress != nil {
			networkAddresses = append(networkAddresses, corev1.NodeAddress{
				Type:    corev1.NodeInternalIP,
				Address: *instance.PrivateIpAddress,
			})
		}
		if instance.PublicDnsName != nil {
			networkAddresses = append(networkAddresses, corev1.NodeAddress{
				Type:    corev1.NodeExternalDNS,
				Address: *instance.PublicDnsName,
			})
		}
		if instance.PrivateDnsName != nil {
			networkAddresses = append(networkAddresses, corev1.NodeAddress{
				Type:    corev1.NodeInternalDNS,
				Address: *instance.PrivateDnsName,
			})
		}
	}
	mLog.Debug("finished calculating AWS status")

	awsStatus.Conditions = SetAWSMachineProviderCondition(awsStatus.Conditions, providerconfigv1.MachineCreation, corev1.ConditionTrue, MachineCreationSucceeded, "machine successfully created", UpdateConditionIfReasonOrMessageChange)

	// TODO(jchaloup): do we really need to update tis?
	// origInstanceID := awsStatus.InstanceID
	// if !StringPtrsEqual(origInstanceID, awsStatus.InstanceID) {
	// 	mLog.Debug("AWS instance ID changed, clearing LastELBSync to trigger adding to ELBs")
	// 	awsStatus.LastELBSync = nil
	// }

	err = a.updateMachineStatus(machine, awsStatus, mLog, networkAddresses)
	if err != nil {
		return err
	}

	// If machine state is still pending, we will return an error to keep the controllers
	// attempting to update status until it hits a more permanent state. This will ensure
	// we get a public IP populated more quickly.
	if awsStatus.InstanceState != nil && *awsStatus.InstanceState == ec2.InstanceStateNamePending {
		mLog.Infof("instance state still pending, returning an error to requeue")
		return &clustererror.RequeueAfterError{RequeueAfter: requeueAfterSeconds * time.Second}
	}
	return nil
}

func getClusterID(machine *clusterv1.Machine) (string, bool) {
	clusterID, ok := machine.Labels[providerconfigv1.ClusterNameLabel]
	return clusterID, ok
}
