package machine

import (
	"fmt"
	"sort"

	providerconfigv1 "sigs.k8s.io/cluster-api-provider-aws/pkg/apis/awsproviderconfig/v1alpha1"
	clusterv1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/glog"

	"encoding/base64"
	"time"
)

// getInstances returns all instances that have a tag matching our machine name,
// and cluster ID.
func (ir *actuatorRuntime) getInstances(machine *clusterv1.Machine, instanceStateFilter []*string) ([]*ec2.Instance, error) {
	machineName := machine.Name

	clusterID, ok := machine.Labels[providerconfigv1.ClusterIDLabel]
	if !ok {
		return nil, fmt.Errorf("unable to get cluster ID for machine: %q", machine.Name)
	}

	requestFilters := []*ec2.Filter{
		{
			Name:   aws.String("tag:Name"),
			Values: []*string{&machineName},
		},
		{
			Name:   aws.String("tag:clusterid"),
			Values: []*string{&clusterID},
		},
	}

	if instanceStateFilter != nil {
		requestFilters = append(requestFilters, &ec2.Filter{
			Name:   aws.String("instance-state-name"),
			Values: instanceStateFilter,
		})
	}

	// Query instances with our machine's name, and in running/pending state.
	request := &ec2.DescribeInstancesInput{
		Filters: requestFilters,
	}

	result, err := ir.awsclient.DescribeInstances(request)
	if err != nil {
		return []*ec2.Instance{}, err
	}

	instances := make([]*ec2.Instance, 0, len(result.Reservations))
	for _, reservation := range result.Reservations {
		for _, instance := range reservation.Instances {
			instances = append(instances, instance)
		}
	}

	return instances, nil
}

// getRunningInstances returns all running instances that have a tag matching our machine name,
// and cluster ID.
func (ir *actuatorRuntime) getRunningInstances(machine *clusterv1.Machine) ([]*ec2.Instance, error) {
	runningInstanceStateFilter := []*string{aws.String(ec2.InstanceStateNameRunning), aws.String(ec2.InstanceStateNamePending)}
	return ir.getInstances(machine, runningInstanceStateFilter)
}

// getStoppedInstances returns all stopped instances that have a tag matching our machine name,
// and cluster ID.
func (ir *actuatorRuntime) getStoppedInstances(machine *clusterv1.Machine) ([]*ec2.Instance, error) {
	stoppedInstanceStateFilter := []*string{aws.String(ec2.InstanceStateNameStopped), aws.String(ec2.InstanceStateNameStopping)}
	return ir.getInstances(machine, stoppedInstanceStateFilter)
}

// terminateInstances terminates all provided instances with a single EC2 request.
func (ir *actuatorRuntime) terminateInstances(instances []*ec2.Instance) error {
	instanceIDs := []*string{}
	// Cleanup all older instances:
	for _, instance := range instances {
		glog.Infof("Cleaning up extraneous instance for machine: %v, state: %v, launchTime: %v", *instance.InstanceId, *instance.State.Name, *instance.LaunchTime)
		instanceIDs = append(instanceIDs, instance.InstanceId)
	}
	for _, instanceID := range instanceIDs {
		glog.Infof("Terminating %v instance", *instanceID)
	}

	terminateInstancesRequest := &ec2.TerminateInstancesInput{
		InstanceIds: instanceIDs,
	}
	_, err := ir.awsclient.TerminateInstances(terminateInstancesRequest)
	if err != nil {
		glog.Errorf("error terminating instances: %v", err)
		return fmt.Errorf("error terminating instances: %v", err)
	}
	return nil
}

// removeStoppedMachine removes all instances of a specific machine that are in a stopped state.
func (ir *actuatorRuntime) removeStoppedMachine(machine *clusterv1.Machine) error {
	instances, err := ir.getStoppedInstances(machine)
	if err != nil {
		return fmt.Errorf("unable to remove stopped instances, error getting stopped instances: %v", err)
	}

	if len(instances) == 0 {
		glog.Infof("no stopped instances found for machine %v", machine.Name)
		return nil
	}

	return ir.terminateInstances(instances)
}

// removeRunningMachine removes all instances of a specific machine that are in a running state.
func (ir *actuatorRuntime) removeRunningMachine(machine *clusterv1.Machine) error {
	instances, err := ir.getRunningInstances(machine)
	if err != nil {
		return fmt.Errorf("unable to remove running instances, error getting running instances: %v", err)
	}

	if len(instances) == 0 {
		glog.Infof("no running instances found for machine %v", machine.Name)
		return nil
	}

	return ir.terminateInstances(instances)
}

func (ir *actuatorRuntime) getAMI(AMI providerconfigv1.AWSResourceReference) (*string, error) {
	if AMI.ID != nil {
		amiID := AMI.ID
		glog.Infof("Using AMI %s", *amiID)
		return amiID, nil
	}
	if len(AMI.Filters) > 0 {
		glog.Info("Describing AMI based on filters")
		describeImagesRequest := ec2.DescribeImagesInput{
			Filters: buildEC2Filters(AMI.Filters),
		}
		describeAMIResult, err := ir.awsclient.DescribeImages(&describeImagesRequest)
		if err != nil {
			return nil, fmt.Errorf("error describing AMI: %v", err)
		}
		if len(describeAMIResult.Images) < 1 {
			return nil, fmt.Errorf("no image for given filters not found")
		}
		latestImage := describeAMIResult.Images[0]
		latestTime, err := time.Parse(time.RFC3339, *latestImage.CreationDate)
		if err != nil {
			return nil, fmt.Errorf("unable to parse time for %q AMI: %v", *latestImage.ImageId, err)
		}
		for _, image := range describeAMIResult.Images[1:] {
			imageTime, err := time.Parse(time.RFC3339, *image.CreationDate)
			if err != nil {
				return nil, fmt.Errorf("unable to parse time for %q AMI: %v", *image.ImageId, err)
			}
			if latestTime.Before(imageTime) {
				latestImage = image
				latestTime = imageTime
			}
		}
		return latestImage.ImageId, nil
	}
	return nil, fmt.Errorf("AMI ID or AMI filters need to be specified")
}

func (ir *actuatorRuntime) getSecurityGroupsIDs(securityGroups []providerconfigv1.AWSResourceReference) ([]*string, error) {
	var securityGroupIDs []*string
	for _, g := range securityGroups {
		// ID has priority
		if g.ID != nil {
			securityGroupIDs = append(securityGroupIDs, g.ID)
		} else if g.Filters != nil {
			glog.Info("Describing security groups based on filters")
			// Get groups based on filters
			describeSecurityGroupsRequest := ec2.DescribeSecurityGroupsInput{
				Filters: buildEC2Filters(g.Filters),
			}
			describeSecurityGroupsResult, err := ir.awsclient.DescribeSecurityGroups(&describeSecurityGroupsRequest)
			if err != nil {
				glog.Errorf("error describing security groups: %v", err)
				return nil, fmt.Errorf("error describing security groups: %v", err)
			}
			for _, g := range describeSecurityGroupsResult.SecurityGroups {
				groupID := *g.GroupId
				securityGroupIDs = append(securityGroupIDs, &groupID)
			}
		}
	}

	if len(securityGroups) == 0 {
		glog.Info("No security group found")
	}

	return securityGroupIDs, nil
}

func (ir *actuatorRuntime) getSubnetIDs(subnet providerconfigv1.AWSResourceReference, availabilityZone string) ([]*string, error) {
	var subnetIDs []*string
	// ID has priority
	if subnet.ID != nil {
		subnetIDs = append(subnetIDs, subnet.ID)
	} else {
		var filters []providerconfigv1.Filter
		if availabilityZone != "" {
			filters = append(filters, providerconfigv1.Filter{Name: "availabilityZone", Values: []string{availabilityZone}})
		}
		filters = append(filters, subnet.Filters...)
		glog.Info("Describing subnets based on filters")
		describeSubnetRequest := ec2.DescribeSubnetsInput{
			Filters: buildEC2Filters(filters),
		}
		describeSubnetResult, err := ir.awsclient.DescribeSubnets(&describeSubnetRequest)
		if err != nil {
			glog.Errorf("error describing subnetes: %v", err)
			return nil, fmt.Errorf("error describing subnets: %v", err)
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

// LaunchInstance launches an instance in AWS
func (ir *actuatorRuntime) launchInstance(machine *clusterv1.Machine, machineProviderConfig *providerconfigv1.AWSMachineProviderConfig, userData []byte) (*ec2.Instance, error) {
	amiID, err := ir.getAMI(machineProviderConfig.AMI)
	if err != nil {
		return nil, err
	}

	securityGroupsIDs, err := ir.getSecurityGroupsIDs(machineProviderConfig.SecurityGroups)
	if err != nil {
		return nil, fmt.Errorf("error getting security groups IDs: %v,", err)
	}
	subnetIDs, err := ir.getSubnetIDs(machineProviderConfig.Subnet, machineProviderConfig.Placement.AvailabilityZone)
	if err != nil {
		return nil, fmt.Errorf("error getting subnet IDs: %v,", err)
	}
	if len(subnetIDs) > 1 {
		glog.Warningf("More than one subnet id returned, only first one will be used")
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

	// Add tags to the created machine
	rawTagList := []*ec2.Tag{}
	for _, tag := range machineProviderConfig.Tags {
		rawTagList = append(rawTagList, &ec2.Tag{Key: aws.String(tag.Name), Value: aws.String(tag.Value)})
	}

	clusterID, ok := machine.Labels[providerconfigv1.ClusterIDLabel]
	if !ok {
		return nil, fmt.Errorf("unable to get cluster ID for machine: %q", machine.Name)
	}

	rawTagList = append(rawTagList, []*ec2.Tag{
		{Key: aws.String("clusterid"), Value: aws.String(clusterID)},
		{Key: aws.String("kubernetes.io/cluster/" + clusterID), Value: aws.String("owned")},
		{Key: aws.String("Name"), Value: aws.String(machine.Name)},
	}...)
	tagList := removeDuplicatedTags(rawTagList)
	tagInstance := &ec2.TagSpecification{
		ResourceType: aws.String("instance"),
		Tags:         tagList,
	}
	tagVolume := &ec2.TagSpecification{
		ResourceType: aws.String("volume"),
		Tags:         []*ec2.Tag{{Key: aws.String("clusterid"), Value: aws.String(clusterID)}},
	}

	userDataEnc := base64.StdEncoding.EncodeToString(userData)

	var iamInstanceProfile *ec2.IamInstanceProfileSpecification
	if machineProviderConfig.IAMInstanceProfile != nil && machineProviderConfig.IAMInstanceProfile.ID != nil {
		iamInstanceProfile = &ec2.IamInstanceProfileSpecification{
			Name: aws.String(*machineProviderConfig.IAMInstanceProfile.ID),
		}
	}

	var placement *ec2.Placement
	if machineProviderConfig.Placement.AvailabilityZone != "" && machineProviderConfig.Subnet.ID == nil {
		placement = &ec2.Placement{
			AvailabilityZone: aws.String(machineProviderConfig.Placement.AvailabilityZone),
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
		Placement:          placement,
	}

	runResult, err := ir.awsclient.RunInstances(&inputConfig)
	if err != nil {
		return nil, fmt.Errorf("error creating EC2 instance: %v", err)
	}

	if runResult == nil || len(runResult.Instances) != 1 {
		return nil, fmt.Errorf("unexpected reservation creating instance")
	}

	return runResult.Instances[0], err
}

type instanceList []*ec2.Instance

func (il instanceList) Len() int {
	return len(il)
}

func (il instanceList) Swap(i, j int) {
	il[i], il[j] = il[j], il[i]
}

func (il instanceList) Less(i, j int) bool {
	if il[i].LaunchTime == nil && il[j].LaunchTime == nil {
		// No idea what to do here, should not be possible, so keep the order
		return false
	}
	if il[i].LaunchTime != nil && il[j].LaunchTime == nil {
		return false
	}
	if il[i].LaunchTime == nil && il[j].LaunchTime != nil {
		return true
	}
	if (*il[i].LaunchTime).After(*il[j].LaunchTime) {
		return true
	}
	return false
}

// sortInstances will sort a list of instance based on an instace launch time
// from the newest to the latest.
// This function should only be called with running instances, not those which are stopped or
// terminated.
func sortInstances(instances []*ec2.Instance) {
	sort.Sort(instanceList(instances))
}

func buildEC2Filters(inputFilters []providerconfigv1.Filter) []*ec2.Filter {
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

// Scan machine tags, and return a deduped tags list
func removeDuplicatedTags(tags []*ec2.Tag) []*ec2.Tag {
	m := make(map[string]bool)
	result := []*ec2.Tag{}

	// look for duplicates
	for _, entry := range tags {
		if _, value := m[*entry.Key]; !value {
			m[*entry.Key] = true
			result = append(result, entry)
		}
	}
	return result
}
