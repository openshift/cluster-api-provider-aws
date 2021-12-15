package machine

import (
	"context"
	"errors"
	"log"
	"testing"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/golang/mock/gomock"
	. "github.com/onsi/gomega"
	configv1 "github.com/openshift/api/config/v1"
	machinev1 "github.com/openshift/machine-api-operator/pkg/apis/machine/v1beta1"
	machinecontroller "github.com/openshift/machine-api-operator/pkg/controller/machine"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes/scheme"
	awsproviderv1 "sigs.k8s.io/cluster-api-provider-aws/pkg/apis/awsprovider/v1beta1"
	awsclient "sigs.k8s.io/cluster-api-provider-aws/pkg/client"
	mockaws "sigs.k8s.io/cluster-api-provider-aws/pkg/client/mock"
	runtimeclient "sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
)

func TestMachineControllerWithDelayedExistSuccess(t *testing.T) {
	ctx := context.TODO()
	g := NewWithT(t)

	mockCtrl := gomock.NewController(t)
	mockAWSClient := mockaws.NewMockClient(mockCtrl)
	awsClientBuilder := func(client runtimeclient.Client, secretName, namespace, region string, configManagedClient runtimeclient.Client) (awsclient.Client, error) {
		return mockAWSClient, nil
	}

	{ // Initialise Mock AWS responses for client
		log.Printf("Initialising mock AWS responses")

		// DesrcibeInstances by tags should only happen before the instance is created/if the provider ID returns nothing
		// For the purpose of this test, don't provide an output, we want to control the provider ID response instead.
		mockAWSClient.EXPECT().DescribeInstances(stubDescribeInstancesInputFromName()).Return(&ec2.DescribeInstancesOutput{}, nil).AnyTimes()

		// Once the actuator has determined the Machine doesn't exist, it should eventually request to create the machine
		mockAWSClient.EXPECT().RunInstances(gomock.Any()).Return(stubReservation("ami-a9acbbd6", stubInstanceID), nil).Times(1)

		// After the create, it will reconcile load balancer attachements, we don't care about these for this test
		mockAWSClient.EXPECT().RegisterInstancesWithLoadBalancer(gomock.Any()).Return(nil, nil).AnyTimes()
		mockAWSClient.EXPECT().ELBv2DescribeLoadBalancers(gomock.Any()).Return(stubDescribeLoadBalancersOutput(), nil).AnyTimes()
		mockAWSClient.EXPECT().ELBv2DescribeTargetGroups(gomock.Any()).Return(stubDescribeTargetGroupsOutput(), nil).AnyTimes()
		mockAWSClient.EXPECT().ELBv2RegisterTargets(gomock.Any()).Return(nil, nil).AnyTimes()
		mockAWSClient.EXPECT().DescribeVpcs(gomock.Any()).Return(StubDescribeVPCs()).AnyTimes()
		mockAWSClient.EXPECT().DescribeDHCPOptions(gomock.Any()).Return(StubDescribeDHCPOptions()).AnyTimes()

		// After create, we will assert that the instance doesn't exist for the first 3 times that the call is made
		// - The first call is Exists, which will return that the instance does not exist
		// - The second is in Create, check for possible eventual consistency errors. This will fail and then the
		//   check for the providerStatus.InstanceID should prevent a second create and requeue.
		// - The third call is Exists on the second reconcile, after which we start returning the instance to allow
		//   the Create eventual consistency error to requeue again, after which Exists will succeed going forward.
		assertNotExist := mockAWSClient.EXPECT().DescribeInstances(stubDescribeInstancesInput(stubInstanceID)).Return(&ec2.DescribeInstancesOutput{}, nil).MaxTimes(3)
		mockAWSClient.EXPECT().DescribeInstances(stubDescribeInstancesInput(stubInstanceID)).Return(stubDescribeInstancesOutput("ami-a9acbbd6", stubInstanceID, ec2.InstanceStateNameRunning, "192.168.0.10"), nil).After(assertNotExist).AnyTimes()

		// Once the machine gets to the update stage, tags will be updated
		mockAWSClient.EXPECT().CreateTags(gomock.Any()).Return(&ec2.CreateTagsOutput{}, nil).AnyTimes()
	}

	var k8sClient runtimeclient.Client
	{ // Set up manager, actuator and controller
		log.Printf("Initialising manager, actuator and controller")
		mgr, err := manager.New(cfg, manager.Options{
			Scheme:             scheme.Scheme,
			MetricsBindAddress: "0",
		})
		g.Expect(err).ToNot(HaveOccurred())

		mgrCtx, cancel := context.WithCancel(context.Background())
		go func() {
			g.Expect(mgr.Start(mgrCtx)).To(Succeed())
		}()
		defer cancel()

		k8sClient = mgr.GetClient()
		eventRecorder := mgr.GetEventRecorderFor("awscontroller")

		params := ActuatorParams{
			Client:           k8sClient,
			EventRecorder:    eventRecorder,
			AwsClientBuilder: awsClientBuilder,
		}
		actuator := NewActuator(params)

		g.Expect(machinecontroller.AddWithActuator(mgr, actuator)).To(Succeed())
	}

	var machine *machinev1.Machine
	var machineKey runtimeclient.ObjectKey
	{ // Init and create the machine and infrastructure object
		log.Printf("Initialising and creating the Machine and supporting resources")
		var err error
		machine, err = stubMachine()
		g.Expect(err).ToNot(HaveOccurred())
		g.Expect(stubMachine).ToNot(BeNil())
		machineKey = runtimeclient.ObjectKey{Namespace: machine.Namespace, Name: machine.Name}

		// Create the machine
		g.Expect(k8sClient.Create(ctx, machine)).To(Succeed())
		defer func() {
			g.Expect(k8sClient.Delete(ctx, machine)).To(Succeed())
		}()

		// Create infrastructure object
		infra := &configv1.Infrastructure{ObjectMeta: metav1.ObjectMeta{Name: awsclient.GlobalInfrastuctureName}}
		g.Expect(k8sClient.Create(ctx, infra)).To(Succeed())
		defer func() {
			g.Expect(k8sClient.Delete(ctx, infra)).To(Succeed())
		}()

		// Create the credentials secret
		awsCredentialsSecret := stubAwsCredentialsSecret()
		g.Expect(k8sClient.Create(context.TODO(), awsCredentialsSecret)).To(Succeed())
		defer func() {
			g.Expect(k8sClient.Delete(context.TODO(), awsCredentialsSecret)).To(Succeed())
		}()

		// Create the user data secret
		userDataSecret := stubUserDataSecret()
		g.Expect(k8sClient.Create(context.TODO(), userDataSecret)).To(Succeed())
		defer func() {
			g.Expect(k8sClient.Delete(context.TODO(), userDataSecret)).To(Succeed())
		}()

		// Ensure the machine has synced to the cache
		getMachine := func() error {
			return k8sClient.Get(ctx, machineKey, machine)
		}
		g.Eventually(getMachine, timeout).Should(Succeed())
	}

	{
		log.Printf("Check expectations of Machine after create")
		// First thing the Machine controller does is move the machine to provisioning and requeue
		waitForProvisioning := func() (bool, error) {
			if err := k8sClient.Get(ctx, machineKey, machine); err != nil {
				return false, err
			}
			return machine.Status.Phase != nil && *machine.Status.Phase == "Provisioning", nil
		}
		g.Eventually(waitForProvisioning, timeout).Should(BeTrue(), "Machine was never moved to provisioning")

		// Then we expect the controller to create the instance and set the instance ID
		waitForInstanceID := func() (bool, error) {
			if err := k8sClient.Get(ctx, machineKey, machine); err != nil {
				return false, err
			}
			if machine.Status.ProviderStatus == nil {
				return false, errors.New("expected providerstatus to not be nil")
			}
			ps, err := awsproviderv1.ProviderStatusFromRawExtension(machine.Status.ProviderStatus)
			if err != nil {
				return false, err
			}
			return ps.InstanceID != nil && *ps.InstanceID == stubInstanceID, nil
		}
		g.Eventually(waitForInstanceID, timeout).Should(BeTrue(), "Instance ID was not set in provider status")
		g.Expect(machine.Spec.ProviderID).To(BeNil(), "Provider ID should not be set after create")
		g.Expect(machine.Status.Addresses).To(BeEmpty(), "Expected addresses to not be set after create")
	}

	{
		log.Printf("Check expectations of Machine after update")
		// First thing the Machine controller does is move the machine to provisioning and requeue
		waitForProvisioned := func() (bool, error) {
			if err := k8sClient.Get(ctx, machineKey, machine); err != nil {
				return false, err
			}
			return machine.Status.Phase != nil && *machine.Status.Phase == "Provisioned", nil
		}
		g.Eventually(waitForProvisioned, timeout).Should(BeTrue(), "Machine was never moved to provisioned")

		g.Expect(machine.Spec.ProviderID).ToNot(BeNil())
		g.Expect(*machine.Spec.ProviderID).To(ContainSubstring(stubInstanceID), "ProviderID should be set after update")
		g.Expect(machine.Status.Addresses).To(HaveLen(4), "Expected addresses to be set after update")
	}
}
