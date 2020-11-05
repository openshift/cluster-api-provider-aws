# AWS ProviderSpec and ProviderStatus documentation

## AWS ProviderSpec

### AMI

[AMI](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/AMIs.html) is the reference to the AMI which serves as templates for your instance and will be used to create the machine instance.

The reference to AMI is presented by ID, ARN, or Filters. Only one of ID, ARN or Filters may be specified.

Example:
```
ami:
  id: ami-0000000
```

### Instance Type 

InstanceType is the type of instance to create. Example: m4.xlarge

[Link to AWS documentation](https://aws.amazon.com/ec2/instance-types/)

### Tags

[Tags](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/Using_Tags.html) is an optional set of tags to add to AWS resources managed by the AWS provider, in addition to the ones added by default.

These set of tags is additive, the controller will ensure these tags are present and not remove any other tags that may exist on the instance.

Each tag should have a `name` and a `value`.

Example usage:

```
tags:
  - name: example1
    value: example1
  - name: example2
    value: example2
```

### IAM Instance Profile

IAM InstanceProfile is a reference to an [IAM role](https://aws.amazon.com/iam/features/manage-roles/#:~:text=IAM%20roles%20allow%20you%20to,to%20make%20AWS%20API%20calls.) to assign to the instance.

The reference to IAM InstanceProfile is presented by ID, ARN, or Filters. Only one of ID, ARN or Filters may be specified.

Example:
```
iamInstanceProfile:
  id: iam-0000000
```

### UserData Secret

UserDataSecret contains a local reference to a secret that contains the UserData to apply to the instance. See [AWS 
documentation](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/user-data.html) for more information on UserData.

### Credentials Secret

CredentialsSecret is a reference to the secret with AWS credentials. Otherwise, defaults to permissions 
provided by attached IAM role where the controller is running.

### KeyName

[KeyName](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/ec2-key-pairs.html) is the name of the KeyPair to use for SSH

### Device Index

DeviceIndex is the index of the device on the instance for the network interface attachment. Defaults to 0.

### Public IP

PublicIP specifies whether the instance should get a public IP. If not present, it should use the default of its subnet.

### Security Groups

SecurityGroups is an array of references to security groups that should be applied to the instance.

The references to Security Groups are presented by ID, ARN, or Filters. Only one of ID, ARN or Filters may be specified.

Example:
```
securityGroups:
- filters:
  - name: tag:Name
    values:
    - example
```

### Subnet

Subnet is a reference to the subnet to use for this instance.

The reference to Subnet is presented by ID, ARN, or Filters. Only one of ID, ARN or Filters may be specified.

Example:
```
subnet:
  filters:
  - name: tag:Name
    values:
    - example
```

### Placement

Placement specifies where to create the instance in AWS. It consists of [a region and availability zone](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-regions-availability-zones.html).

Example:

```
placement:
  availabilityZone: us-west-1a
  region: us-west-1
```

### Load Balancers

[LoadBalancers](https://aws.amazon.com/elasticloadbalancing/) is the set of load balancers to which the new instance should be added once it is created. The list should contain load balancer's name and type. Only two types are allowed: classic and network.

```
loadBalancers:
 - name: example1
   type: classic
 - name: example1
   type: network
```

### Block Devices

[BlockDevices](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/block-device-mapping-concepts.html) is the set of block device mapping associated to this instance, block device without a name will be used as a root device and only one device without a name is allowed.

Block device mapping spec consists of following fields: 
 - DeviceName - The device name exposed to the machine (for example, /dev/sdh or xvdh).
 - EBS - Parameters used to automatically set up EBS volumes when the machine is launched.
 - NoDevice - Suppresses the specified device included in the block device mapping of the AMI.
 - VirtualName - The virtual device name (ephemeralN). Machine store volumes are numbered starting from 0. An machine type with 2 available machine store volumes can specify mappings for ephemeral0 and ephemeral1. The number of available
 machine store volumes depends on the machine type. After you connect to the machine, you must mount the volume.
 Constraints: For M3 machines, you must specify machine store volumes in the block device mapping for the machine. 
 When you launch an M3 machine, we ignore any machine store volumes specified in the block device mapping
 for the AMI.

EBS parameters contains of following fields:
 - DeleteOnTermination - indicates whether the EBS volume is deleted on machine termination.
 - Encrypted - indicates whether the EBS volume is encrypted. Encrypted Amazon EBS volumes may only be attached to machines that support Amazon EBS encryption.
 - KMSKey AWSResourceReference - indicates the KMS key that should be used to encrypt the Amazon EBS volume. The reference to KMSKey is presented by ID, ARN, or Filters. Only one of ID, ARN or Filters may be specified.
 - Iops - the number of I/O operations per second (IOPS) that the volume supports. For io1, this represents the number of IOPS that are provisioned for the volume. For gp2, this represents the baseline performance of the volume and the rate at which the volume accumulates I/O credits for bursting. For more information about General Purpose SSD baseline performance, I/O credits, and bursting, see [AWS EBS Volume Types](http://docs.aws.amazon.com/AWSEC2/latest/UserGuide/EBSVolumeTypes.html). Minimal and maximal IOPS for io1 and gp2 are constrained. Please, check this [link](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/EBSVolumeTypes.html) for precise boundaries for individual volumes.
 Condition: This parameter is required for requests to create io1 volumes, it is not used in requests to create gp2, st1, sc1, or standard volumes.
 - VolumeSize - the size of the volume, in GiB.
	 Constraints: 1-16384 for General Purpose SSD (gp2), 4-16384 for Provisioned IOPS SSD (io1), 500-16384 
   for Throughput Optimized HDD (st1), 500-16384 for Cold HDD (sc1), and 1-1024 for Magnetic (standard) 
   volumes. If you specify a snapshot, the volume size must be equal to or larger than the snapshot size.
	 Default: If you're creating the volume from a snapshot and don't specify
	 a volume size, the default is the snapshot size.
 - VolumeType - the volume type: gp2, io1, st1, sc1, or standard.
 Default: standard

Example:
```
blockDevices:
- ebs:
    encrypted: true
    iops: 0
    kmsKey:
      arn: "example-0000000"
    volumeSize: 120
    volumeType: gp2
```

### Spot Market Options

[SpotMarketOptions](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/using-spot-instances.html) allows users to configure instances to be run using AWS Spot instances. For more information on spot instances see this [proposal](https://github.com/openshift/enhancements/blob/master/enhancements/machine-api/spot-instances.md).

SpotMarketOptions field contains the maximum price the user is willing to pay for their instances. Default: On-Demand price.

Example:

```
spotMarketOptions:
 maxPrice: 10
```

### Tenancy 

[Tenancy](https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/dedicated-instance.html) indicates if instance should run on shared or single-tenant hardware. There are supported 3 options: default, dedicated and host.


### AWS ProviderStatus

AWS ProviderStatus will be embedded in a Machine.Status.ProviderStatus. It contains AWS-specific status
information.

#### InstanceID

InstanceID is the instance ID of the machine created in AWS.

#### InstanceState

InstanceState is the state of the AWS instance for this machine.

#### Conditions

Conditions is a set of conditions associated with the Machine to indicate errors or other status.
