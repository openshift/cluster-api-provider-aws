package machines

import (
	"fmt"
	"testing"

	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/util/wait"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"github.com/prometheus/common/log"
	"k8s.io/apimachinery/pkg/util/uuid"

	"github.com/openshift/cluster-api-actuator-pkg/pkg/e2e/framework"
	"github.com/openshift/cluster-api-actuator-pkg/pkg/manifests"
	"sigs.k8s.io/cluster-api-provider-aws/test/utils"

	apiv1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	clusterv1alpha1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"

	machineutils "sigs.k8s.io/cluster-api-provider-aws/pkg/cloud/aws/actuators/machine"
	awsclient "sigs.k8s.io/cluster-api-provider-aws/pkg/cloud/aws/client"
)

const (
	region                   = "us-east-1"
	awsCredentialsSecretName = "aws-credentials-secret"
)

func TestCart(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Machine Suite")
}

var _ = framework.SigKubeDescribe("Machines", func() {
	var testNamespace *apiv1.Namespace
	f, err := framework.NewFramework()
	if err != nil {
		panic(fmt.Errorf("unable to create framework: %v", err))
	}

	machinesToDelete := framework.InitMachinesToDelete()

	BeforeEach(func() {
		f.BeforeEach()

		testNamespace = &apiv1.Namespace{
			ObjectMeta: metav1.ObjectMeta{
				Name: "namespace-" + string(uuid.NewUUID()),
			},
		}

		By(fmt.Sprintf("Creating %q namespace", testNamespace.Name))
		_, err = f.KubeClient.CoreV1().Namespaces().Create(testNamespace)
		Expect(err).NotTo(HaveOccurred())

		f.DeployClusterAPIStack(testNamespace.Name, f.ActuatorImage, "")
	})

	AfterEach(func() {
		// Make sure all machine(set)s are deleted before deleting its namespace
		machinesToDelete.Delete()

		if testNamespace != nil {
			f.DestroyClusterAPIStack(testNamespace.Name, f.ActuatorImage, "")
			log.Infof(testNamespace.Name+": %#v", testNamespace)
			By(fmt.Sprintf("Destroying %q namespace", testNamespace.Name))
			f.KubeClient.CoreV1().Namespaces().Delete(testNamespace.Name, &metav1.DeleteOptions{})
			// Ignore namespaces that are not deleted so other specs can be run.
			// Every spec runs in its own namespaces so it's enough to make sure
			// namespaces does not inluence each other
		}
		// it's assumed the cluster API is completely destroyed
	})

	// Any of the tests run assumes the cluster-api stack is already deployed.
	// So all the machine, resp. machineset related tests must be run on top
	// of the same cluster-api stack. Once the machine, resp. machineset objects
	// are defined through CRD, we can relax the restriction.
	Context("AWS actuator", func() {
		var (
			acw           framework.CloudProviderClient
			awsClient     awsclient.Client
			awsCredSecret *apiv1.Secret
			cluster       *clusterv1alpha1.Cluster
			clusterID     string
		)

		BeforeEach(func() {
			awsCredSecret = utils.GenerateAwsCredentialsSecretFromEnv(awsCredentialsSecretName, testNamespace.Name)
			createSecretAndWait(f, awsCredSecret)

			clusterID = framework.ClusterID
			if clusterID == "" {
				clusterID = "cluster-" + string(uuid.NewUUID())
			}

			cluster = &clusterv1alpha1.Cluster{
				ObjectMeta: metav1.ObjectMeta{
					Name:      clusterID,
					Namespace: testNamespace.Name,
				},
				Spec: clusterv1alpha1.ClusterSpec{
					ClusterNetwork: clusterv1alpha1.ClusterNetworkingConfig{
						Services: clusterv1alpha1.NetworkRanges{
							CIDRBlocks: []string{"10.0.0.1/24"},
						},
						Pods: clusterv1alpha1.NetworkRanges{
							CIDRBlocks: []string{"10.0.0.1/24"},
						},
						ServiceDomain: "example.com",
					},
				},
			}
			f.CreateClusterAndWait(cluster)

			var err error
			awsClient, err = awsclient.NewClient(f.KubeClient, awsCredSecret.Name, awsCredSecret.Namespace, region)
			Expect(err).NotTo(HaveOccurred())
			acw = machineutils.NewAwsClientWrapper(awsClient)

		})

		It("Can create AWS instances", func() {
			// Create/delete a single machine, test instance is provisioned/terminated
			testMachineProviderConfig, err := utils.TestingMachineProviderConfig(awsCredSecret.Name, cluster.Name)
			Expect(err).NotTo(HaveOccurred())
			testMachine := manifests.TestingMachine(cluster.Name, cluster.Namespace, testMachineProviderConfig)
			f.CreateMachineAndWait(testMachine, acw)
			machinesToDelete.AddMachine(testMachine, f, acw)

			By("Checking subnet", func() {
				describeSubnetsInput := &ec2.DescribeSubnetsInput{
					Filters: []*ec2.Filter{
						{
							Name: aws.String("tag:Name"),
							Values: []*string{
								aws.String(fmt.Sprintf("%s-*", clusterID)),
							},
						},
						{
							Name: aws.String("availabilityZone"),
							Values: []*string{
								aws.String("us-east-1a"),
							},
						},
					},
				}
				describeSubnetsResult, err := awsClient.DescribeSubnets(describeSubnetsInput)
				Expect(err).NotTo(HaveOccurred())
				Expect(len(describeSubnetsResult.Subnets)).
					To(Equal(1), "Test criteria not met. Only one Subnet should match the given Tag.")
				subnetID := *describeSubnetsResult.Subnets[0].SubnetId
				subnet, err := acw.GetSubnet(testMachine)
				Expect(err).NotTo(HaveOccurred())
				Expect(subnet).To(Equal(subnetID))
			})

			By("Checking availability zone", func() {
				availabilityZone, err := acw.GetAvailabilityZone(testMachine)
				Expect(err).NotTo(HaveOccurred())
				Expect(availabilityZone).To(Equal("us-east-1a"))
			})

			By("Checking security groups", func() {
				securityGroups, err := acw.GetSecurityGroups(testMachine)
				Expect(err).NotTo(HaveOccurred())
				Expect(securityGroups).To(Equal([]string{fmt.Sprintf("%s-default", clusterID)}))
			})

			By("Checking IAM role", func() {
				iamRole, err := acw.GetIAMRole(testMachine)
				Expect(err).NotTo(HaveOccurred())
				Expect(iamRole).To(Equal(""))
			})

			By("Checking tags", func() {
				tags, err := acw.GetTags(testMachine)
				Expect(err).NotTo(HaveOccurred())
				Expect(tags).To(Equal(map[string]string{
					fmt.Sprintf("kubernetes.io/cluster/%s", clusterID): "owned",
					"openshift-node-group-config":                      "node-config-master",
					"sub-host-type":                                    "default",
					"host-type":                                        "master",
					"Name":                                             testMachine.Name,
					"clusterid":                                        clusterID,
				}))
			})

			By("Checking machine status", func() {
				condition := getMachineCondition(f, testMachine)
				Expect(condition.Reason).To(Equal(machineutils.MachineCreationSucceeded))
			})

			f.DeleteMachineAndWait(testMachine, acw)
		})

		It("Can deploy compute nodes through machineset", func() {
			// Any controller linking kubernetes node with its machine
			// needs to run inside the cluster where the node is registered.
			// Which means the cluster API stack needs to be deployed in the same
			// cluster in order to list machine object(s) defining the node.
			//
			// One could run the controller inside the current cluster and have
			// new nodes join the cluster assumming the cluster was created with kubeadm
			// and the bootstrapping token is known in advance. Though, in case of AWS
			// all instances must live in the same vpc, otherwise additional configuration
			// of the underlying environment is required. Which does not have to be
			// available.

			// Thus:
			// 1. create testing cluster (deploy master node)
			// 2. deploy the cluster-api inside the master node
			// 3. deploy machineset with worker nodes
			// 4. check all worker nodes has compute role and corresponding machines
			//    are linked to them

			// Create master machine and verify the master node is ready
			masterUserDataSecret, err := manifests.MasterMachineUserDataSecret(
				"masteruserdatasecret",
				testNamespace.Name,
				[]string{"\\$(curl -s http://169.254.169.254/latest/meta-data/public-hostname)", "\\$(curl -s http://169.254.169.254/latest/meta-data/public-ipv4)"},
			)
			Expect(err).NotTo(HaveOccurred())

			createSecretAndWait(f, masterUserDataSecret)
			masterMachineProviderConfig, err := utils.MasterMachineProviderConfig(awsCredSecret.Name, masterUserDataSecret.Name, cluster.Name)
			Expect(err).NotTo(HaveOccurred())
			masterMachine := manifests.MasterMachine(cluster.Name, cluster.Namespace, masterMachineProviderConfig)
			f.CreateMachineAndWait(masterMachine, acw)
			machinesToDelete.AddMachine(masterMachine, f, acw)

			By("Collecting master kubeconfig")
			restConfig, err := f.GetMasterMachineRestConfig(masterMachine, acw)
			Expect(err).NotTo(HaveOccurred())

			// Load actuator docker image to the master node
			dnsName, err := acw.GetPublicDNSName(masterMachine)
			Expect(err).NotTo(HaveOccurred())

			err = f.UploadDockerImageToInstance(f.ActuatorImage, dnsName)
			Expect(err).NotTo(HaveOccurred())

			// Deploy the cluster API stack inside the master machine
			sshConfig, err := framework.DefaultSSHConfig()
			Expect(err).NotTo(HaveOccurred())
			clusterFramework, err := framework.NewFrameworkFromConfig(restConfig, sshConfig)
			Expect(err).NotTo(HaveOccurred())
			By(fmt.Sprintf("Creating %q namespace", testNamespace.Name))
			_, err = clusterFramework.KubeClient.CoreV1().Namespaces().Create(testNamespace)
			Expect(err).NotTo(HaveOccurred())

			clusterFramework.DeployClusterAPIStack(testNamespace.Name, f.ActuatorImage, "")

			By("Deploy worker nodes through machineset")
			masterPrivateIP, err := acw.GetPrivateIP(masterMachine)
			Expect(err).NotTo(HaveOccurred())

			// Reuse the namespace, secret and the cluster objects
			clusterFramework.CreateClusterAndWait(cluster)
			createSecretAndWait(clusterFramework, awsCredSecret)

			workerUserDataSecret, err := manifests.WorkerMachineUserDataSecret("workeruserdatasecret", testNamespace.Name, masterPrivateIP)
			Expect(err).NotTo(HaveOccurred())
			createSecretAndWait(clusterFramework, workerUserDataSecret)
			workerMachineSetProviderConfig, err := utils.WorkerMachineSetProviderConfig(awsCredSecret.Name, workerUserDataSecret.Name, cluster.Name)
			Expect(err).NotTo(HaveOccurred())
			workerMachineSet := manifests.WorkerMachineSet(cluster.Name, cluster.Namespace, workerMachineSetProviderConfig)
			fmt.Printf("workerMachineSet: %#v\n", workerMachineSet)
			clusterFramework.CreateMachineSetAndWait(workerMachineSet, acw)
			machinesToDelete.AddMachineSet(workerMachineSet, clusterFramework, acw)

			By("Checking master and worker nodes are ready")
			err = clusterFramework.WaitForNodesToGetReady(2)
			Expect(err).NotTo(HaveOccurred())

			By("Checking compute node role and node linking")
			err = wait.Poll(framework.PollInterval, framework.PoolTimeout, func() (bool, error) {
				items, err := clusterFramework.KubeClient.CoreV1().Nodes().List(metav1.ListOptions{})
				if err != nil {
					return false, fmt.Errorf("unable to list nodes: %v", err)
				}

				var nonMasterNodes []apiv1.Node
				for _, node := range items.Items {
					// filter out all nodes with master role (assumming it's always set)
					if _, isMaster := node.Labels["node-role.kubernetes.io/master"]; isMaster {
						continue
					}
					nonMasterNodes = append(nonMasterNodes, node)
				}

				machines, err := clusterFramework.CAPIClient.ClusterV1alpha1().Machines(workerMachineSet.Namespace).List(metav1.ListOptions{
					LabelSelector: labels.SelectorFromSet(workerMachineSet.Spec.Selector.MatchLabels).String(),
				})
				Expect(err).NotTo(HaveOccurred())

				matches := make(map[string]string)
				for _, machine := range machines.Items {
					if machine.Status.NodeRef != nil {
						matches[machine.Status.NodeRef.Name] = machine.Name
					}
				}

				// non-master node, the workerset deploys only compute nodes
				for _, node := range nonMasterNodes {
					// check role
					_, isCompute := node.Labels["node-role.kubernetes.io/compute"]
					if !isCompute {
						log.Infof("node %q does not have the compute role assigned", node.Name)
						return false, nil
					}
					log.Infof("node %q role set to 'node-role.kubernetes.io/compute'", node.Name)
					// check node linking

					// If there is the same number of machines are compute nodes,
					// each node has to have a machine that refers the node.
					// So it's enough to check each node has its machine linked.
					matchingMachine, found := matches[node.Name]
					if !found {
						log.Infof("node %q is not linked with a machine", node.Name)
						return false, nil
					}
					log.Infof("node %q is linked with %q machine", node.Name, matchingMachine)
				}

				return true, nil
			})
			Expect(err).NotTo(HaveOccurred())

			By("Destroying worker machines")
			// Let it fail and continue (assuming all instances gets removed out of the e2e)
			clusterFramework.DeleteMachineSetAndWait(workerMachineSet, acw)
			By("Destroying master machine")
			f.DeleteMachineAndWait(masterMachine, acw)
		})

	})

})
