package main

import (
	"fmt"
	"io/ioutil"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/golang/glog"
	log "github.com/sirupsen/logrus"
	"github.com/spf13/cobra"
	awsclient "sigs.k8s.io/cluster-api-provider-aws/cloud/aws/client"
	clusterapiaproviderawsv1alpha1 "sigs.k8s.io/cluster-api-provider-aws/cloud/aws/providerconfig"

	"github.com/kubernetes-incubator/apiserver-builder/pkg/controller"
	appsv1beta2 "k8s.io/api/apps/v1beta2"
	apiv1 "k8s.io/api/core/v1"
	rbacv1 "k8s.io/api/rbac/v1"
	apiextensionsclientset "k8s.io/apiextensions-apiserver/pkg/client/clientset/clientset"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/util/intstr"
	"k8s.io/apimachinery/pkg/util/wait"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	apiregistrationv1beta1 "k8s.io/kube-aggregator/pkg/apis/apiregistration/v1beta1"
	apiregistrationclientset "k8s.io/kube-aggregator/pkg/client/clientset_generated/clientset"
	clusterv1alpha1 "sigs.k8s.io/cluster-api/pkg/apis/cluster/v1alpha1"
	clusterapiclientset "sigs.k8s.io/cluster-api/pkg/client/clientset_generated/clientset"
)

const (
	pollInterval                            = 1 * time.Second
	timeoutPoolAWSInterval                  = 25 * time.Second
	timeoutPoolClusterAPIDeploymentInterval = 10 * time.Minute
	timeoutPoolMachineRunningInterval       = 10 * time.Minute

	defaultLogLevel          = "info"
	targetNamespace          = "default"
	awsCredentialsSecretName = "aws-credentials-secret"
	region                   = "us-east-1"
)

func usage() {
	fmt.Printf("Usage: %s\n\n", os.Args[0])
}

// TestConfig stores clients for managing various resources
type TestConfig struct {
	CAPIClient            *clusterapiclientset.Clientset
	APIExtensionsClient   *apiextensionsclientset.Clientset
	APIRegistrationClient *apiregistrationclientset.Clientset
	KubeClient            *kubernetes.Clientset
	AWSClient             awsclient.Client
}

// NewTestConfig creates new test config with clients
func NewTestConfig(kubeconfig string) *TestConfig {
	config, err := controller.GetConfig(kubeconfig)
	if err != nil {
		glog.Fatalf("Could not create Config for talking to the apiserver: %v", err)
	}

	capiclient, err := clusterapiclientset.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Could not create client for talking to the apiserver: %v", err)
	}

	apiextensionsclient, err := apiextensionsclientset.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Could not create client for talking to the apiserver: %v", err)
	}

	apiregistrationclient, err := apiregistrationclientset.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Could not create client for talking to the apiserver: %v", err)
	}

	kubeClient, err := kubernetes.NewForConfig(config)
	if err != nil {
		glog.Fatalf("Could not create kubernetes client to talk to the apiserver: %v", err)
	}

	return &TestConfig{
		CAPIClient:            capiclient,
		APIExtensionsClient:   apiextensionsclient,
		APIRegistrationClient: apiregistrationclient,
		KubeClient:            kubeClient,
	}
}

func createNamespace(testConfig *TestConfig, namespace string) error {
	nsObj := &apiv1.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: namespace,
		},
	}

	log.Infof("Creating %q namespace...", nsObj.Name)
	if _, err := testConfig.KubeClient.CoreV1().Namespaces().Get(nsObj.Name, metav1.GetOptions{}); err != nil {
		if _, err := testConfig.KubeClient.CoreV1().Namespaces().Create(nsObj); err != nil {
			return fmt.Errorf("unable to create namespace: %v", err)
		}
	}

	return nil
}

func createSecret(testConfig *TestConfig, secret *apiv1.Secret) error {
	log.Infof("Creating %q secret...", strings.Join([]string{secret.Namespace, secret.Name}, "/"))
	if _, err := testConfig.KubeClient.CoreV1().Secrets(secret.Namespace).Get(secret.Name, metav1.GetOptions{}); err != nil {
		if _, err := testConfig.KubeClient.CoreV1().Secrets(secret.Namespace).Create(secret); err != nil {
			return fmt.Errorf("unable to create secret: %v", err)
		}
	}

	return nil
}

func createDeployment(testConfig *TestConfig, deployment *appsv1beta2.Deployment) error {
	log.Infof("Creating %q deployment...", strings.Join([]string{deployment.Namespace, deployment.Name}, "/"))
	deploymentsClient := testConfig.KubeClient.AppsV1beta2().Deployments(deployment.Namespace)
	if _, err := deploymentsClient.Get(deployment.Name, metav1.GetOptions{}); err != nil {
		if _, err := deploymentsClient.Create(deployment); err != nil {
			return fmt.Errorf("unable to create Deployment: %v", err)
		}
	}

	return nil
}

func createAPIService(testConfig *TestConfig, apiService *apiregistrationv1beta1.APIService) error {
	log.Infof("Creating %q api service...", apiService.Name)
	apiServiceClient := testConfig.APIRegistrationClient.Apiregistration().APIServices()
	if _, err := apiServiceClient.Get(apiService.Name, metav1.GetOptions{}); err != nil {
		if _, err := apiServiceClient.Create(apiService); err != nil {
			return fmt.Errorf("unable to create API service: %v", err)
		}
	}
	return nil
}

func createService(testConfig *TestConfig, service *apiv1.Service) error {
	log.Infof("Creating %q service...", strings.Join([]string{service.Namespace, service.Name}, "/"))
	serviceClient := testConfig.KubeClient.CoreV1().Services(service.Namespace)
	if _, err := serviceClient.Get(service.Name, metav1.GetOptions{}); err != nil {
		if _, err := serviceClient.Create(service); err != nil {
			return fmt.Errorf("unable to create service: %v", err)
		}
	}
	return nil
}

func createRoleBinding(testConfig *TestConfig, roleBinding *rbacv1.RoleBinding) error {
	log.Infof("Creating %q role binding...", strings.Join([]string{roleBinding.Namespace, roleBinding.Name}, "/"))
	roleBindingClient := testConfig.KubeClient.RbacV1().RoleBindings(roleBinding.Namespace)
	if _, err := roleBindingClient.Get(roleBinding.Name, metav1.GetOptions{}); err != nil {
		if _, err := roleBindingClient.Create(roleBinding); err != nil {
			return fmt.Errorf("unable to create role binding: %v", err)
		}
	}
	return nil
}

func createStatefulSet(testConfig *TestConfig, statefulSet *appsv1beta2.StatefulSet) error {
	log.Infof("Creating %q stateful set...", strings.Join([]string{statefulSet.Namespace, statefulSet.Name}, "/"))
	statefulSetClient := testConfig.KubeClient.AppsV1beta2().StatefulSets(statefulSet.Namespace)
	if _, err := statefulSetClient.Get(statefulSet.Name, metav1.GetOptions{}); err != nil {
		if _, err := statefulSetClient.Create(statefulSet); err != nil {
			return fmt.Errorf("unable to create stateful set: %v", err)
		}
	}
	return nil
}

func createCluster(testConfig *TestConfig, cluster *clusterv1alpha1.Cluster) error {
	log.Infof("Creating %q cluster...", strings.Join([]string{cluster.Namespace, cluster.Name}, "/"))
	clusterClient := testConfig.CAPIClient.ClusterV1alpha1().Clusters(cluster.Namespace)
	if _, err := clusterClient.Get(cluster.Name, metav1.GetOptions{}); err != nil {
		if _, err := clusterClient.Create(cluster); err != nil {
			return fmt.Errorf("unable to create cluster: %v", err)
		}
	}
	return nil
}

func createMachine(testConfig *TestConfig, machine *clusterv1alpha1.Machine) error {
	log.Infof("Creating %q machine...", strings.Join([]string{machine.Namespace, machine.Name}, "/"))
	machineClient := testConfig.CAPIClient.ClusterV1alpha1().Machines(machine.Namespace)
	if _, err := machineClient.Get(machine.Name, metav1.GetOptions{}); err != nil {
		if _, err := machineClient.Create(machine); err != nil {
			return fmt.Errorf("unable to create machine: %v", err)
		}
	}
	return nil
}

func cmdRun(assetsDir string, binaryPath string, args ...string) error {
	cmd := exec.Command(binaryPath, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Dir = assetsDir
	return cmd.Run()
}

var rootCmd = &cobra.Command{
	Use:   "e2e",
	Short: "Test deployment of cluster-api stack",
	RunE: func(cmd *cobra.Command, args []string) error {
		kubeconfig := cmd.Flag("kubeconfig").Value.String()
		logLevel := cmd.Flag("log-level").Value.String()
		assetsPath := cmd.Flag("assets-path").Value.String()
		terraformPath := cmd.Flag("terraform-path").Value.String()
		clusterID := cmd.Flag("cluster-id").Value.String()
		machineName := fmt.Sprintf("aws-actuator-testing-machine-%s", clusterID)
		testNamespace := "test"
		awsUser := cmd.Flag("aws-user").Value.String()

		if kubeconfig == "" {
			return fmt.Errorf("--kubeconfig option is required")
		}

		log.SetOutput(os.Stdout)
		if lvl, err := log.ParseLevel(logLevel); err != nil {
			log.Panic(err)
		} else {
			log.SetLevel(lvl)
		}

		testConfig := NewTestConfig(kubeconfig)

		// create terraform stub enviroment
		if err := cmdRun(terraformPath, "terraform", "init"); err != nil {
			glog.Fatalf("unable to run terraform init: %v", err)
		}
		tfVarEnvironmentID := fmt.Sprintf("environment_id=%s", clusterID)
		tfVarAWSUser := fmt.Sprintf("aws_user=%s", awsUser)
		if err := cmdRun(terraformPath, "terraform", "apply", "-var", tfVarEnvironmentID, "-var", tfVarAWSUser, "-input=false", "-auto-approve"); err != nil {
			glog.Fatalf("unable to run terraform apply -auto-approve: %v", err)
		}
		defer tearDown(testConfig, assetsPath, machineName)

		// generate aws creds kube secret
		if err := cmdRun(assetsPath, "./generate.sh"); err != nil {
			glog.Fatalf("unable to run generate.sh: %v", err)
		}

		// generate assumed namespaces
		if err := createNamespace(testConfig, testNamespace); err != nil {
			return err
		}

		decode := scheme.Codecs.UniversalDeserializer().Decode

		// create aws creds secret
		if secretData, err := ioutil.ReadFile(filepath.Join(assetsPath, "manifests/aws-credentials.yaml")); err != nil {
			glog.Fatalf("Error reading %#v", err)
		} else {
			secretObj, _, err := decode([]byte(secretData), nil, nil)
			if err != nil {
				glog.Fatalf("Error decoding %#v", err)
			}
			secret := secretObj.(*apiv1.Secret)
			if err := createSecret(testConfig, secret); err != nil {
				return err
			}
		}

		// create cluster apiserver certs secret
		if secretData, err := ioutil.ReadFile(filepath.Join(assetsPath, "manifests/cluster-apiserver-certs.yaml")); err != nil {
			glog.Fatalf("Error reading %#v", err)
		} else {
			secretObj, _, err := decode([]byte(secretData), nil, nil)
			if err != nil {
				glog.Fatalf("Error decoding %#v", err)
			}
			secret := secretObj.(*apiv1.Secret)
			if err := createSecret(testConfig, secret); err != nil {
				return err
			}
		}

		awsClient, err := awsclient.NewClient(testConfig.KubeClient, awsCredentialsSecretName, targetNamespace, region)
		if err != nil {
			glog.Fatalf("Could not create aws client: %v", err)
		}
		testConfig.AWSClient = awsClient

		apiService := &apiregistrationv1beta1.APIService{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "v1alpha1.cluster.k8s.io",
				Namespace: testNamespace,
				Labels: map[string]string{
					"api":       "clusterapi",
					"apiserver": "true",
				},
			},
			Spec: apiregistrationv1beta1.APIServiceSpec{
				Version:              "v1alpha1",
				Group:                "cluster.k8s.io",
				GroupPriorityMinimum: 2000,
				Service: &apiregistrationv1beta1.ServiceReference{
					Name:      "clusterapi",
					Namespace: "default",
				},
				VersionPriority: 10,
				CABundle:        []byte("LS0tLS1CRUdJTiBDRVJUSUZJQ0FURS0tLS0tCk1JSUM5RENDQWR5Z0F3SUJBZ0lCQURBTkJna3Foa2lHOXcwQkFRc0ZBREFyTVNrd0p3WURWUVFERXlCamJIVnoKZEdWeVlYQnBMV05sY25ScFptbGpZWFJsTFdGMWRHaHZjbWwwZVRBZUZ3MHhPREE0TURreE5EUXhOVFZhRncweQpPREE0TURZeE5EUXhOVFZhTUNzeEtUQW5CZ05WQkFNVElHTnNkWE4wWlhKaGNHa3RZMlZ5ZEdsbWFXTmhkR1V0CllYVjBhRzl5YVhSNU1JSUJJakFOQmdrcWhraUc5dzBCQVFFRkFBT0NBUThBTUlJQkNnS0NBUUVBNXU3QkdCODUKKzNPT0NIV0NCQjYrWFNSTkNxelhTazBCUFo5YjBsbkdBZGVLNHdxc2h2SUhhZ0N2WHRGZEx3Q21zMVBXbzNHMApUM2ExY2h6TVBSUFNwV01tcldDei9Ibm1zOG45RkVjZi9BRGJXcmw4V2I5akJlbHNlWGlQS2dyYVY4dnp6N0xHCnJvdDZMV2Q0RjVUaUo4Q2FuaGl3dTFqT1RkK2FiNHRxc0ZDU05CZDRzTXc0Y2loejNjVzZoNnZmTlJtZ2VDaUcKNXp4S2RRTlBNT08rZ01rQjZJUXZPcG5MdllEMWxDTkRMTHY0c3NKNncxbUxlVHZGME9BT3A5NWxuT3k0MUtJSgpWK1RaVGlMZHZ5aFdRMmJEekwvbG9vNitHSUJ5NWJMeGdBV3FUcGQ3blU5NElzWEpuQ1NQR20zUzVOLy96TjJSCnlzTTU1cnlSNlVMWlpRSURBUUFCb3lNd0lUQU9CZ05WSFE4QkFmOEVCQU1DQXFRd0R3WURWUjBUQVFIL0JBVXcKQXdFQi96QU5CZ2txaGtpRzl3MEJBUXNGQUFPQ0FRRUEyQnI3MGNMT1BXM1g3bHpKdXBlOFpBTUM4Uk4rMUtwTgoxLzF2NitSNU1xMEtnSlFzTnFnUC9ZQWRUdGFvcVRsaGVhMnFMenBPSDBmNHlNZWthckhPd2VxZjVLL1JwLzdoCk1vNG9jT1ZUZm13OStWZmZ4Nk9RVHhxTTZ1dkszSXd6ZlBJa25hMWFsS3pBTmlxVkM5UTg0NExzMG42RDJDazUKK245Um82TUd5d1gybkVvUDd2bFJHdnB3ejExV0ZjcWNPTWp3WTV1aUlpdUlSOGhTNmpOSmJ6OUgvME5nNTB3egpOSFJOc3ltWkdvTEtYMDBBbjJyVVZ5ME53TllyNDAwR0VRQnVwcEtsNHJaNmw1UFR4N2ZoNy8zd0FOM0pmWEUwClA2NlFvY081WnJYQXF3ZXpva215bWp4MzFraElnRnZObDhUYzFXaEVmN01PbWRTd1duUUNQdz09Ci0tLS0tRU5EIENFUlRJRklDQVRFLS0tLS0K"),
			},
		}

		if err := createAPIService(testConfig, apiService); err != nil {
			return err
		}

		service := &apiv1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "clusterapi",
				Namespace: "default",
				Labels: map[string]string{
					"api":       "clusterapi",
					"apiserver": "true",
				},
			},
			Spec: apiv1.ServiceSpec{
				Ports: []apiv1.ServicePort{
					{
						Port:       443,
						Protocol:   apiv1.ProtocolTCP,
						TargetPort: intstr.FromInt(443),
					},
				},
				Selector: map[string]string{
					"api":       "clusterapi",
					"apiserver": "true",
				},
			},
		}

		if err := createService(testConfig, service); err != nil {
			return err
		}

		var replicas int32 = 1
		apiserverDeployment := &appsv1beta2.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "clusterapi-apiserver",
				Namespace: "default",
				Labels: map[string]string{
					"api":       "clusterapi",
					"apiserver": "true",
				},
			},
			Spec: appsv1beta2.DeploymentSpec{
				Replicas: &replicas,
				Template: apiv1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"api":       "clusterapi",
							"apiserver": "true",
						},
					},
					Spec: apiv1.PodSpec{
						NodeSelector: map[string]string{
							"node-role.kubernetes.io/master": "",
						},
						Tolerations: []apiv1.Toleration{
							{
								Effect: apiv1.TaintEffectNoSchedule,
								Key:    "node-role.kubernetes.io/master",
							},
							{
								Key:      "CriticalAddonsOnly",
								Operator: "Exists",
							},
							{
								Effect:   apiv1.TaintEffectNoExecute,
								Key:      "node.alpha.kubernetes.io/notReady",
								Operator: "Exists",
							},
							{
								Effect:   apiv1.TaintEffectNoExecute,
								Key:      "node.alpha.kubernetes.io/unreachable",
								Operator: "Exists",
							},
						},
						Containers: []apiv1.Container{
							{
								Name:  "apiserver",
								Image: "gcr.io/k8s-cluster-api/cluster-apiserver:0.0.6",
								VolumeMounts: []apiv1.VolumeMount{
									{
										Name:      "cluster-apiserver-certs",
										MountPath: "/apiserver.local.config/certificates",
										ReadOnly:  true,
									},
									{
										Name:      "config",
										MountPath: "/etc/kubernetes",
									},
									{
										Name:      "certs",
										MountPath: "/etc/ssl/certs",
									},
								},
								Command: []string{"./apiserver"},
								Args: []string{
									"--etcd-servers=http://etcd-clusterapi-svc:2379",
									"--tls-cert-file=/apiserver.local.config/certificates/tls.crt",
									"--tls-private-key-file=/apiserver.local.config/certificates/tls.key",
									"--audit-log-path=-",
									"--audit-log-maxage=0",
									"--audit-log-maxbackup=0",
									"--authorization-kubeconfig=/etc/kubernetes/admin.conf",
									"--kubeconfig=/etc/kubernetes/admin.conf",
								},
								Resources: apiv1.ResourceRequirements{
									Requests: apiv1.ResourceList{
										"cpu":    resource.MustParse("100m"),
										"memory": resource.MustParse("50Mi"),
									},
									Limits: apiv1.ResourceList{
										"cpu":    resource.MustParse("300m"),
										"memory": resource.MustParse("200Mi"),
									},
								},
							},
						},
						Volumes: []apiv1.Volume{
							{
								Name: "cluster-apiserver-certs",
								VolumeSource: apiv1.VolumeSource{
									Secret: &apiv1.SecretVolumeSource{
										SecretName: "cluster-apiserver-certs",
									},
								},
							},
							{
								Name: "config",
								VolumeSource: apiv1.VolumeSource{
									HostPath: &apiv1.HostPathVolumeSource{
										Path: "/etc/kubernetes",
									},
								},
							},
							{
								Name: "certs",
								VolumeSource: apiv1.VolumeSource{
									HostPath: &apiv1.HostPathVolumeSource{
										Path: "/etc/ssl/certs",
									},
								},
							},
						},
					},
				},
			},
		}

		if err := createDeployment(testConfig, apiserverDeployment); err != nil {
			return err
		}

		controllerDeployment := &appsv1beta2.Deployment{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "clusterapi-controllers",
				Namespace: "default",
				Labels: map[string]string{
					"api": "clusterapi",
				},
			},
			Spec: appsv1beta2.DeploymentSpec{
				Replicas: &replicas,
				Template: apiv1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"api": "clusterapi",
						},
					},
					Spec: apiv1.PodSpec{
						NodeSelector: map[string]string{
							"node-role.kubernetes.io/master": "",
						},
						Tolerations: []apiv1.Toleration{
							{
								Effect: apiv1.TaintEffectNoSchedule,
								Key:    "node-role.kubernetes.io/master",
							},
							{
								Key:      "CriticalAddonsOnly",
								Operator: "Exists",
							},
							{
								Effect:   apiv1.TaintEffectNoExecute,
								Key:      "node.alpha.kubernetes.io/notReady",
								Operator: "Exists",
							},
							{
								Effect:   apiv1.TaintEffectNoExecute,
								Key:      "node.alpha.kubernetes.io/unreachable",
								Operator: "Exists",
							},
						},
						Containers: []apiv1.Container{
							{
								Name:  "controller-manager",
								Image: "gcr.io/k8s-cluster-api/aws-machine-controller:0.0.1",
								VolumeMounts: []apiv1.VolumeMount{
									{
										Name:      "config",
										MountPath: "/etc/kubernetes",
									},
									{
										Name:      "certs",
										MountPath: "/etc/ssl/certs",
									},
								},
								Command: []string{"./controller-manager"},
								Args:    []string{"--kubeconfig=/etc/kubernetes/admin.conf"},
								Resources: apiv1.ResourceRequirements{
									Requests: apiv1.ResourceList{
										"cpu":    resource.MustParse("100m"),
										"memory": resource.MustParse("20Mi"),
									},
									Limits: apiv1.ResourceList{
										"cpu":    resource.MustParse("100m"),
										"memory": resource.MustParse("30Mi"),
									},
								},
							},
							{
								Name:  "aws-machine-controller",
								Image: "gcr.io/k8s-cluster-api/aws-machine-controller:0.0.1",
								VolumeMounts: []apiv1.VolumeMount{
									{
										Name:      "config",
										MountPath: "/etc/kubernetes",
									},
									{
										Name:      "certs",
										MountPath: "/etc/ssl/certs",
									},
									{
										Name:      "kubeadm",
										MountPath: "/usr/bin/kubeadm",
									},
								},
								Env: []apiv1.EnvVar{
									{
										Name: "NODE_NAME",
										ValueFrom: &apiv1.EnvVarSource{
											FieldRef: &apiv1.ObjectFieldSelector{
												FieldPath: "spec.nodeName",
											},
										},
									},
								},
								Command: []string{"./machine-controller"},
								Args: []string{
									"--log-level=debug",
									"--kubeconfig=/etc/kubernetes/admin.conf",
								},
								Resources: apiv1.ResourceRequirements{
									Requests: apiv1.ResourceList{
										"cpu":    resource.MustParse("100m"),
										"memory": resource.MustParse("20Mi"),
									},
									Limits: apiv1.ResourceList{
										"cpu":    resource.MustParse("100m"),
										"memory": resource.MustParse("30Mi"),
									},
								},
							},
						},
						Volumes: []apiv1.Volume{
							{
								Name: "config",
								VolumeSource: apiv1.VolumeSource{
									HostPath: &apiv1.HostPathVolumeSource{
										Path: "/etc/kubernetes",
									},
								},
							},
							{
								Name: "certs",
								VolumeSource: apiv1.VolumeSource{
									HostPath: &apiv1.HostPathVolumeSource{
										Path: "/etc/ssl/certs",
									},
								},
							},
							{
								Name: "kubeadm",
								VolumeSource: apiv1.VolumeSource{
									HostPath: &apiv1.HostPathVolumeSource{
										Path: "/usr/bin/kubeadm",
									},
								},
							},
						},
					},
				},
			},
		}

		if err := createDeployment(testConfig, controllerDeployment); err != nil {
			return err
		}

		roleBinding := &rbacv1.RoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "clusterapi",
				Namespace: "kube-system",
			},
			RoleRef: rbacv1.RoleRef{
				APIGroup: "rbac.authorization.k8s.io",
				Kind:     "default",
				Name:     "default",
			},
			Subjects: []rbacv1.Subject{
				{
					Kind:      "ServiceAccount",
					Name:      "default",
					Namespace: "default",
				},
			},
		}

		if err := createRoleBinding(testConfig, roleBinding); err != nil {
			return err
		}

		var terminationGracePeriodSeconds int64 = 10
		hostPathDirectoryOrCreate := apiv1.HostPathDirectoryOrCreate
		statefulSet := &appsv1beta2.StatefulSet{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "etcd-clusterapi",
				Namespace: "default",
			},
			Spec: appsv1beta2.StatefulSetSpec{
				ServiceName: "etcd",
				Replicas:    &replicas,
				Template: apiv1.PodTemplateSpec{
					ObjectMeta: metav1.ObjectMeta{
						Labels: map[string]string{
							"app": "etcd",
						},
					},
					Spec: apiv1.PodSpec{
						NodeSelector: map[string]string{
							"node-role.kubernetes.io/master": "",
						},
						Tolerations: []apiv1.Toleration{
							{
								Effect: apiv1.TaintEffectNoSchedule,
								Key:    "node-role.kubernetes.io/master",
							},
							{
								Key:      "CriticalAddonsOnly",
								Operator: "Exists",
							},
							{
								Effect:   apiv1.TaintEffectNoExecute,
								Key:      "node.alpha.kubernetes.io/notReady",
								Operator: "Exists",
							},
							{
								Effect:   apiv1.TaintEffectNoExecute,
								Key:      "node.alpha.kubernetes.io/unreachable",
								Operator: "Exists",
							},
						},
						Containers: []apiv1.Container{
							{
								Name:  "etcd",
								Image: "k8s.gcr.io/etcd:3.1.12",
								VolumeMounts: []apiv1.VolumeMount{
									{
										Name:      "etcd-data-dir",
										MountPath: "/etcd-data-dir",
									},
								},
								Env: []apiv1.EnvVar{
									{
										Name:  "ETCD_DATA_DIR",
										Value: "/etcd-data-dir",
									},
								},
								Command: []string{
									"/usr/local/bin/etcd",
									"--listen-client-urls",
									"http://0.0.0.0:2379",
									"--advertise-client-urls",
									"http://localhost:2379",
								},
								Ports: []apiv1.ContainerPort{
									{
										ContainerPort: 2379,
									},
								},
								Resources: apiv1.ResourceRequirements{
									Requests: apiv1.ResourceList{
										"cpu":    resource.MustParse("100m"),
										"memory": resource.MustParse("50Mi"),
									},
									Limits: apiv1.ResourceList{
										"cpu":    resource.MustParse("200m"),
										"memory": resource.MustParse("300Mi"),
									},
								},
								ReadinessProbe: &apiv1.Probe{
									Handler: apiv1.Handler{
										HTTPGet: &apiv1.HTTPGetAction{
											Port: intstr.FromInt(2379),
											Path: "/health",
										},
									},
									InitialDelaySeconds: 10,
									TimeoutSeconds:      2,
									PeriodSeconds:       10,
									SuccessThreshold:    1,
									FailureThreshold:    1,
								},
								LivenessProbe: &apiv1.Probe{
									Handler: apiv1.Handler{
										HTTPGet: &apiv1.HTTPGetAction{
											Port: intstr.FromInt(2379),
											Path: "/health",
										},
									},
									InitialDelaySeconds: 10,
									TimeoutSeconds:      2,
									PeriodSeconds:       10,
									SuccessThreshold:    1,
									FailureThreshold:    3,
								},
							},
						},
						Volumes: []apiv1.Volume{
							{
								Name: "etcd-data-dir",
								VolumeSource: apiv1.VolumeSource{
									HostPath: &apiv1.HostPathVolumeSource{
										Path: "/etc/kubernetes",
										Type: &hostPathDirectoryOrCreate,
									},
								},
							},
						},
						TerminationGracePeriodSeconds: &terminationGracePeriodSeconds,
					},
				},
			},
		}

		if err := createStatefulSet(testConfig, statefulSet); err != nil {
			return err
		}

		etcdService := &apiv1.Service{
			ObjectMeta: metav1.ObjectMeta{
				Name:      "etcd-clusterapi-svc",
				Namespace: "default",
				Labels: map[string]string{
					"app": "etcd",
				},
			},
			Spec: apiv1.ServiceSpec{
				Ports: []apiv1.ServicePort{
					{
						Port:       2379,
						TargetPort: intstr.FromInt(2379),
						Name:       "etcd",
					},
				},
				Selector: map[string]string{
					"app": "etcd",
				},
			},
		}

		if err := createService(testConfig, etcdService); err != nil {
			return err
		}

		err = wait.Poll(pollInterval, timeoutPoolClusterAPIDeploymentInterval, func() (bool, error) {
			if clusterAPIDeployment, err := testConfig.KubeClient.AppsV1beta2().Deployments("default").Get("clusterapi-apiserver", metav1.GetOptions{}); err == nil {
				// Check all the pods are running
				log.Infof("Waiting for all cluster-api deployment pods to be ready, have %v, expecting 1", clusterAPIDeployment.Status.ReadyReplicas)
				if clusterAPIDeployment.Status.ReadyReplicas < 1 {
					return false, nil
				}
				return true, nil
			}

			log.Info("Waiting for cluster-api deployment to be created")
			return false, nil
		})

		if err != nil {
			return err
		}

		log.Info("The cluster-api stack is ready")

		c := &clusterv1alpha1.Cluster{
			ObjectMeta: metav1.ObjectMeta{
				Name:      clusterID,
				Namespace: testNamespace,
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

		if err := createCluster(testConfig, c); err != nil {
			return err
		}

		publicIP := true
		amiID := "ami-a9acbbd6"
		iamInstanceProfileID := "openshift_master_launch_instances"
		a := &clusterapiaproviderawsv1alpha1.AWSMachineProviderConfig{
			AMI: clusterapiaproviderawsv1alpha1.AWSResourceReference{
				ID: &amiID,
			},
			CredentialsSecret: &apiv1.LocalObjectReference{
				Name: awsCredentialsSecretName,
			},
			InstanceType: "m4.xlarge",
			Placement: clusterapiaproviderawsv1alpha1.Placement{
				Region:           "us-east-1",
				AvailabilityZone: "us-east-1a",
			},
			Subnet: clusterapiaproviderawsv1alpha1.AWSResourceReference{
				Filters: []clusterapiaproviderawsv1alpha1.Filter{
					{
						Name:   "tag:Name",
						Values: []string{fmt.Sprintf("%s-worker-*", clusterID)},
					},
				},
			},
			IAMInstanceProfile: &clusterapiaproviderawsv1alpha1.AWSResourceReference{
				ID: &iamInstanceProfileID,
			},
			Tags: []clusterapiaproviderawsv1alpha1.TagSpecification{
				{
					Name:  "openshift-node-group-config",
					Value: "node-config-master",
				},
				{
					Name:  "host-type",
					Value: "master",
				},
				{
					Name:  "sub-host-type",
					Value: "default",
				},
			},
			SecurityGroups: []clusterapiaproviderawsv1alpha1.AWSResourceReference{
				{
					Filters: []clusterapiaproviderawsv1alpha1.Filter{
						{
							Name:   "tag:Name",
							Values: []string{fmt.Sprintf("%s-*", clusterID)},
						},
					},
				},
			},
			PublicIP: &publicIP,
		}

		m := &clusterv1alpha1.Machine{
			ObjectMeta: metav1.ObjectMeta{
				Name:         machineName,
				Namespace:    testNamespace,
				GenerateName: "vs-master-",
				Labels: map[string]string{
					"sigs.k8s.io/cluster-api-cluster":      "tb-asg-35",
					"sigs.k8s.io/cluster-api-machine-role": "infra",
					"sigs.k8s.io/cluster-api-machine-type": "master",
				},
			},
			Spec: clusterv1alpha1.MachineSpec{
				ProviderConfig: clusterv1alpha1.ProviderConfig{
					Value: &runtime.RawExtension{
						Object: a,
					},
				},
				Versions: clusterv1alpha1.MachineVersionInfo{
					Kubelet:      "1.10.1",
					ControlPlane: "1.10.1",
				},
			},
		}

		if err := createMachine(testConfig, m); err != nil {
			return err
		}

		// Verify cluster and machine have been deployed
		var cluster, machine bool
		err = wait.Poll(pollInterval, timeoutPoolMachineRunningInterval, func() (bool, error) {
			if _, err := testConfig.CAPIClient.ClusterV1alpha1().Clusters(testNamespace).Get(clusterID, metav1.GetOptions{}); err == nil {
				cluster = true
				log.Info("Cluster object has been created")
			}

			if _, err := testConfig.CAPIClient.ClusterV1alpha1().Machines(testNamespace).Get(machineName, metav1.GetOptions{}); err == nil {
				machine = true
				log.Info("Machine object has been created")
			}

			if cluster && machine {
				return true, nil
			}
			log.Info("Waiting for cluster and machine to be created")
			return false, nil
		})

		if err != nil {
			return err
		}

		log.Info("The cluster and the machine have been deployed")

		err = wait.Poll(pollInterval, timeoutPoolAWSInterval, func() (bool, error) {
			log.Info("Waiting for aws instances to come up")
			runningInstances, err := testConfig.AWSClient.GetRunningInstances(clusterID)
			if err != nil {
				return false, fmt.Errorf("unable to get running instances from aws: %v", err)
			}
			if len(runningInstances) == 1 {
				log.Info("Machine is running on aws")
				return true, nil
			}
			return false, nil
		})

		if err != nil {
			return err
		}

		log.Info("All verified successfully. Tearing down...")
		return nil
	},
}

func tearDown(testConfig *TestConfig, assetsPath, machineName string) error {
	// delete machine
	// not erroring here so we try to terraform destroy
	if err := testConfig.CAPIClient.ClusterV1alpha1().Machines(targetNamespace).Delete(machineName, &metav1.DeleteOptions{}); err != nil {
		log.Warningf("unable to delete machine, %v", err)
	}

	// delete terraform stub environment
	log.Info("Running terraform destroy")
	if err := cmdRun(assetsPath, "terraform", "destroy", "-force"); err != nil {
		return fmt.Errorf("unable run terraform destroy: %v", err)
	}
	return nil
}

func init() {
	rootCmd.PersistentFlags().StringP("kubeconfig", "m", "", "Kubernetes config")
	rootCmd.PersistentFlags().StringP("log-level", "l", defaultLogLevel, "Log level (debug,info,warn,error,fatal)")
	rootCmd.PersistentFlags().StringP("assets-path", "", "./test/e2e", "path to kube assets")
	rootCmd.PersistentFlags().StringP("terraform-path", "", "./hack/prebuild", "path to terraform and kube assets")
	rootCmd.PersistentFlags().StringP("cluster-id", "", "testCluster", "A unique id for the environment to build")
	rootCmd.PersistentFlags().StringP("aws-user", "", "", "Tags resources with the given user name")
}

func main() {
	err := rootCmd.Execute()
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error occurred: %v\n", err)
		os.Exit(1)
	}
}
