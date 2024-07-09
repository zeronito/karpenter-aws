/*
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

package ami_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	awssdk "github.com/aws/aws-sdk-go/aws"

	corev1 "sigs.k8s.io/karpenter/pkg/apis/v1"

	providerv1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/awslabs/operatorpkg/status"
	. "github.com/awslabs/operatorpkg/test/expectations"
	"github.com/samber/lo"
	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	environmentaws "github.com/aws/karpenter-provider-aws/test/pkg/environment/aws"

	coretest "sigs.k8s.io/karpenter/pkg/test"
)

var env *environmentaws.Environment
var nodeClass *providerv1.EC2NodeClass
var nodePool *corev1.NodePool

func TestAMI(t *testing.T) {
	RegisterFailHandler(Fail)
	BeforeSuite(func() {
		env = environmentaws.NewEnvironment(t)
	})
	AfterSuite(func() {
		env.Stop()
	})
	RunSpecs(t, "Ami")
}

var _ = BeforeEach(func() {
	env.BeforeEach()
	nodeClass = env.DefaultEC2NodeClass()
	nodePool = env.DefaultNodePool(nodeClass)
})
var _ = AfterEach(func() { env.Cleanup() })
var _ = AfterEach(func() { env.AfterEach() })

var _ = Describe("AMI", func() {
	var customAMI string
	BeforeEach(func() {
		customAMI = env.GetAMIBySSMPath(fmt.Sprintf("/aws/service/eks/optimized-ami/%s/amazon-linux-2023/x86_64/standard/recommended/image_id", env.K8sVersion()))
	})

	It("should use the AMI defined by the AMI Selector Terms", func() {
		pod := coretest.Pod()
		nodeClass.Spec.AMISelectorTerms = []providerv1.AMISelectorTerm{
			{
				ID: customAMI,
			},
		}
		env.ExpectCreated(pod, nodeClass, nodePool)
		env.EventuallyExpectHealthy(pod)
		env.ExpectCreatedNodeCount("==", 1)

		env.ExpectInstance(pod.Spec.NodeName).To(HaveField("ImageId", HaveValue(Equal(customAMI))))
	})
	It("should use the most recent AMI when discovering multiple", func() {
		// choose an old static image that will definitely have an older creation date
		oldCustomAMI := env.GetAMIBySSMPath(fmt.Sprintf("/aws/service/eks/optimized-ami/%[1]s/amazon-linux-2023/x86_64/standard/amazon-eks-node-al2023-x86_64-standard-%[1]s-v20240514/image_id", env.K8sVersion()))
		nodeClass.Spec.AMISelectorTerms = []providerv1.AMISelectorTerm{
			{
				ID: customAMI,
			},
			{
				ID: oldCustomAMI,
			},
		}
		pod := coretest.Pod()

		env.ExpectCreated(pod, nodeClass, nodePool)
		env.EventuallyExpectHealthy(pod)
		env.ExpectCreatedNodeCount("==", 1)

		env.ExpectInstance(pod.Spec.NodeName).To(HaveField("ImageId", HaveValue(Equal(customAMI))))
	})
	It("should support AMI Selector Terms for Name but fail with incorrect owners", func() {
		output, err := env.EC2API.DescribeImages(&ec2.DescribeImagesInput{
			ImageIds: []*string{awssdk.String(customAMI)},
		})
		Expect(err).To(BeNil())
		Expect(output.Images).To(HaveLen(1))
		nodeClass.Spec.AMISelectorTerms = []providerv1.AMISelectorTerm{
			{
				Name:  *output.Images[0].Name,
				Owner: "fakeOwnerValue",
			},
		}
		pod := coretest.Pod()

		env.ExpectCreated(pod, nodeClass, nodePool)
		env.ExpectCreatedNodeCount("==", 0)
		Expect(pod.Spec.NodeName).To(Equal(""))
	})
	It("should support ami selector Name with default owners", func() {
		output, err := env.EC2API.DescribeImages(&ec2.DescribeImagesInput{
			ImageIds: []*string{awssdk.String(customAMI)},
		})
		Expect(err).To(BeNil())
		Expect(output.Images).To(HaveLen(1))

		nodeClass.Spec.AMISelectorTerms = []providerv1.AMISelectorTerm{
			{
				Name: *output.Images[0].Name,
			},
		}
		pod := coretest.Pod()

		env.ExpectCreated(pod, nodeClass, nodePool)
		env.EventuallyExpectHealthy(pod)
		env.ExpectCreatedNodeCount("==", 1)

		env.ExpectInstance(pod.Spec.NodeName).To(HaveField("ImageId", HaveValue(Equal(customAMI))))
	})
	It("should support ami selector ids", func() {
		nodeClass.Spec.AMISelectorTerms = []providerv1.AMISelectorTerm{
			{
				ID: customAMI,
			},
		}
		pod := coretest.Pod()

		env.ExpectCreated(pod, nodeClass, nodePool)
		env.EventuallyExpectHealthy(pod)
		env.ExpectCreatedNodeCount("==", 1)

		env.ExpectInstance(pod.Spec.NodeName).To(HaveField("ImageId", HaveValue(Equal(customAMI))))
	})

	Context("AMIFamily", func() {
		It("should provision a node using the AL2 family", func() {
			pod := coretest.Pod()
			nodeClass.Spec.AMIFamily = &providerv1.AMIFamilyAL2
			env.ExpectCreated(nodeClass, nodePool, pod)
			env.EventuallyExpectHealthy(pod)
			env.ExpectCreatedNodeCount("==", 1)
		})
		It("should provision a node using the AL2023 family", func() {
			nodeClass.Spec.AMIFamily = &providerv1.AMIFamilyAL2023
			pod := coretest.Pod()
			env.ExpectCreated(nodeClass, nodePool, pod)
			env.EventuallyExpectHealthy(pod)
			env.ExpectCreatedNodeCount("==", 1)
		})
		It("should provision a node using the Bottlerocket family", func() {
			nodeClass.Spec.AMIFamily = &providerv1.AMIFamilyBottlerocket
			pod := coretest.Pod()
			env.ExpectCreated(nodeClass, nodePool, pod)
			env.EventuallyExpectHealthy(pod)
			env.ExpectCreatedNodeCount("==", 1)
		})
		It("should provision a node using the Ubuntu family", func() {
			nodeClass.Spec.AMIFamily = &providerv1.AMIFamilyUbuntu
			// TODO (jmdeal@): remove once 22.04 AMIs are supported
			if env.K8sMinorVersion() >= 29 {
				nodeClass.Spec.AMISelectorTerms = lo.Map([]string{
					"/aws/service/canonical/ubuntu/eks/20.04/1.28/stable/current/amd64/hvm/ebs-gp2/ami-id",
					"/aws/service/canonical/ubuntu/eks/20.04/1.28/stable/current/arm64/hvm/ebs-gp2/ami-id",
				}, func(ssmPath string, _ int) providerv1.AMISelectorTerm {
					return providerv1.AMISelectorTerm{ID: env.GetAMIBySSMPath(ssmPath)}
				})
			}
			pod := coretest.Pod()
			env.ExpectCreated(nodeClass, nodePool, pod)
			env.EventuallyExpectHealthy(pod)
			env.ExpectCreatedNodeCount("==", 1)
		})
		It("should support Custom AMIFamily with AMI Selectors", func() {
			nodeClass.Spec.AMIFamily = &providerv1.AMIFamilyCustom
			al2023AMI := env.GetAMIBySSMPath(fmt.Sprintf("/aws/service/eks/optimized-ami/%s/amazon-linux-2023/x86_64/standard/recommended/image_id", env.K8sVersion()))
			nodeClass.Spec.AMISelectorTerms = []providerv1.AMISelectorTerm{
				{
					ID: al2023AMI,
				},
			}
			rawContent, err := os.ReadFile("testdata/al2023_userdata_input.yaml")
			Expect(err).ToNot(HaveOccurred())
			nodeClass.Spec.UserData = lo.ToPtr(fmt.Sprintf(string(rawContent), env.ClusterName,
				env.ClusterEndpoint, env.ExpectCABundle()))
			pod := coretest.Pod()

			env.ExpectCreated(pod, nodeClass, nodePool)
			env.EventuallyExpectHealthy(pod)
			env.ExpectCreatedNodeCount("==", 1)

			env.ExpectInstance(pod.Spec.NodeName).To(HaveField("ImageId", HaveValue(Equal(al2023AMI))))
		})
		It("should have the EC2NodeClass status for AMIs using wildcard", func() {
			nodeClass.Spec.AMISelectorTerms = []providerv1.AMISelectorTerm{
				{
					Name: "*",
				},
			}
			env.ExpectCreated(nodeClass)
			nc := EventuallyExpectAMIsToExist(nodeClass)
			Expect(len(nc.Status.AMIs)).To(BeNumerically("<", 10))
		})
		It("should have the EC2NodeClass status for AMIs using tags", func() {
			nodeClass.Spec.AMISelectorTerms = []providerv1.AMISelectorTerm{
				{
					ID: customAMI,
				},
			}
			env.ExpectCreated(nodeClass)
			nc := EventuallyExpectAMIsToExist(nodeClass)
			Expect(len(nc.Status.AMIs)).To(BeNumerically("==", 1))
			Expect(nc.Status.AMIs[0].ID).To(Equal(customAMI))
			ExpectStatusConditions(env, env.Client, 1*time.Minute, nodeClass, status.Condition{Type: v1beta1.ConditionTypeAMIsReady, Status: metav1.ConditionTrue})
			ExpectStatusConditions(env, env.Client, 1*time.Minute, nodeClass, status.Condition{Type: status.ConditionReady, Status: metav1.ConditionTrue})
		})
		It("should have ec2nodeClass status as not ready since AMI was not resolved", func() {
			nodeClass.Spec.AMISelectorTerms = []providerv1.AMISelectorTerm{
				{
					ID: "ami-123",
				},
			}
			env.ExpectCreated(nodeClass)
			ExpectStatusConditions(env, env.Client, 1*time.Minute, nodeClass, status.Condition{Type: v1beta1.ConditionTypeAMIsReady, Status: metav1.ConditionFalse, Message: "AMISelector did not match any AMIs"})
			ExpectStatusConditions(env, env.Client, 1*time.Minute, nodeClass, status.Condition{Type: status.ConditionReady, Status: metav1.ConditionFalse, Message: "AMIsReady=False"})
		})
	})

	Context("UserData", func() {
		It("should merge UserData contents for AL2 AMIFamily", func() {
			content, err := os.ReadFile("testdata/al2_userdata_input.sh")
			Expect(err).ToNot(HaveOccurred())
			nodeClass.Spec.AMIFamily = &providerv1.AMIFamilyAL2
			nodeClass.Spec.UserData = awssdk.String(string(content))
			nodePool.Spec.Template.Spec.Taints = []v1.Taint{{Key: "example.com", Value: "value", Effect: "NoExecute"}}
			nodePool.Spec.Template.Spec.StartupTaints = []v1.Taint{{Key: "example.com", Value: "value", Effect: "NoSchedule"}}
			pod := coretest.Pod(coretest.PodOptions{Tolerations: []v1.Toleration{{Key: "example.com", Operator: v1.TolerationOpExists}}})

			env.ExpectCreated(pod, nodeClass, nodePool)
			env.EventuallyExpectHealthy(pod)
			Expect(env.GetNode(pod.Spec.NodeName).Spec.Taints).To(ContainElements(
				v1.Taint{Key: "example.com", Value: "value", Effect: "NoExecute"},
				v1.Taint{Key: "example.com", Value: "value", Effect: "NoSchedule"},
			))
			actualUserData, err := base64.StdEncoding.DecodeString(*getInstanceAttribute(pod.Spec.NodeName, "userData").UserData.Value)
			Expect(err).ToNot(HaveOccurred())
			// Since the node has joined the cluster, we know our bootstrapping was correct.
			// Just verify if the UserData contains our custom content too, rather than doing a byte-wise comparison.
			Expect(string(actualUserData)).To(ContainSubstring("Running custom user data script"))
		})
		It("should merge non-MIME UserData contents for AL2 AMIFamily", func() {
			content, err := os.ReadFile("testdata/al2_no_mime_userdata_input.sh")
			Expect(err).ToNot(HaveOccurred())
			nodeClass.Spec.AMIFamily = &providerv1.AMIFamilyAL2
			nodeClass.Spec.UserData = awssdk.String(string(content))
			nodePool.Spec.Template.Spec.Taints = []v1.Taint{{Key: "example.com", Value: "value", Effect: "NoExecute"}}
			nodePool.Spec.Template.Spec.StartupTaints = []v1.Taint{{Key: "example.com", Value: "value", Effect: "NoSchedule"}}
			pod := coretest.Pod(coretest.PodOptions{Tolerations: []v1.Toleration{{Key: "example.com", Operator: v1.TolerationOpExists}}})

			env.ExpectCreated(pod, nodeClass, nodePool)
			env.EventuallyExpectHealthy(pod)
			Expect(env.GetNode(pod.Spec.NodeName).Spec.Taints).To(ContainElements(
				v1.Taint{Key: "example.com", Value: "value", Effect: "NoExecute"},
				v1.Taint{Key: "example.com", Value: "value", Effect: "NoSchedule"},
			))
			actualUserData, err := base64.StdEncoding.DecodeString(*getInstanceAttribute(pod.Spec.NodeName, "userData").UserData.Value)
			Expect(err).ToNot(HaveOccurred())
			// Since the node has joined the cluster, we know our bootstrapping was correct.
			// Just verify if the UserData contains our custom content too, rather than doing a byte-wise comparison.
			Expect(string(actualUserData)).To(ContainSubstring("Running custom user data script"))
		})
		It("should merge UserData contents for Bottlerocket AMIFamily", func() {
			content, err := os.ReadFile("testdata/br_userdata_input.sh")
			Expect(err).ToNot(HaveOccurred())
			nodeClass.Spec.AMIFamily = &providerv1.AMIFamilyBottlerocket
			nodeClass.Spec.UserData = awssdk.String(string(content))
			nodePool.Spec.Template.Spec.Taints = []v1.Taint{{Key: "example.com", Value: "value", Effect: "NoExecute"}}
			nodePool.Spec.Template.Spec.StartupTaints = []v1.Taint{{Key: "example.com", Value: "value", Effect: "NoSchedule"}}
			pod := coretest.Pod(coretest.PodOptions{Tolerations: []v1.Toleration{{Key: "example.com", Operator: v1.TolerationOpExists}}})

			env.ExpectCreated(pod, nodeClass, nodePool)
			env.EventuallyExpectHealthy(pod)
			Expect(env.GetNode(pod.Spec.NodeName).Spec.Taints).To(ContainElements(
				v1.Taint{Key: "example.com", Value: "value", Effect: "NoExecute"},
				v1.Taint{Key: "example.com", Value: "value", Effect: "NoSchedule"},
			))
			actualUserData, err := base64.StdEncoding.DecodeString(*getInstanceAttribute(pod.Spec.NodeName, "userData").UserData.Value)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(actualUserData)).To(ContainSubstring("kube-api-qps = 30"))
		})
		// Windows tests are can flake due to the instance types that are used in testing.
		// The VPC Resource controller will need to support the instance types that are used.
		// If the instance type is not supported by the controller resource `vpc.amazonaws.com/PrivateIPv4Address` will not register.
		// Issue: https://github.com/aws/karpenter-provider-aws/issues/4472
		// See: https://github.com/aws/amazon-vpc-resource-controller-k8s/blob/master/pkg/aws/vpc/limits.go
		It("should merge UserData contents for Windows AMIFamily", func() {
			env.ExpectWindowsIPAMEnabled()
			DeferCleanup(func() {
				env.ExpectWindowsIPAMDisabled()
			})

			content, err := os.ReadFile("testdata/windows_userdata_input.ps1")
			Expect(err).ToNot(HaveOccurred())
			nodeClass.Spec.AMIFamily = &providerv1.AMIFamilyWindows2022
			nodeClass.Spec.UserData = awssdk.String(string(content))
			nodePool.Spec.Template.Spec.Taints = []v1.Taint{{Key: "example.com", Value: "value", Effect: "NoExecute"}}
			nodePool.Spec.Template.Spec.StartupTaints = []v1.Taint{{Key: "example.com", Value: "value", Effect: "NoSchedule"}}

			nodePool = coretest.ReplaceRequirements(nodePool,
				corev1.NodeSelectorRequirementWithMinValues{
					NodeSelectorRequirement: v1.NodeSelectorRequirement{
						Key:      v1.LabelOSStable,
						Operator: v1.NodeSelectorOpIn,
						Values:   []string{string(v1.Windows)},
					},
				},
			)
			pod := coretest.Pod(coretest.PodOptions{
				Image: environmentaws.WindowsDefaultImage,
				NodeSelector: map[string]string{
					v1.LabelOSStable:     string(v1.Windows),
					v1.LabelWindowsBuild: "10.0.20348",
				},
				Tolerations: []v1.Toleration{{Key: "example.com", Operator: v1.TolerationOpExists}},
			})

			env.ExpectCreated(pod, nodeClass, nodePool)
			env.EventuallyExpectHealthyWithTimeout(time.Minute*15, pod) // Wait 15 minutes because Windows nodes/containers take longer to spin up
			Expect(env.GetNode(pod.Spec.NodeName).Spec.Taints).To(ContainElements(
				v1.Taint{Key: "example.com", Value: "value", Effect: "NoExecute"},
				v1.Taint{Key: "example.com", Value: "value", Effect: "NoSchedule"},
			))
			actualUserData, err := base64.StdEncoding.DecodeString(*getInstanceAttribute(pod.Spec.NodeName, "userData").UserData.Value)
			Expect(err).ToNot(HaveOccurred())
			Expect(string(actualUserData)).To(ContainSubstring("Write-Host \"Running custom user data script\""))
			Expect(string(actualUserData)).To(ContainSubstring("[string]$EKSBootstrapScriptFile = \"$env:ProgramFiles\\Amazon\\EKS\\Start-EKSBootstrap.ps1\""))
		})
	})
})

//nolint:unparam
func getInstanceAttribute(nodeName string, attribute string) *ec2.DescribeInstanceAttributeOutput {
	var node v1.Node
	Expect(env.Client.Get(env.Context, types.NamespacedName{Name: nodeName}, &node)).To(Succeed())
	providerIDSplit := strings.Split(node.Spec.ProviderID, "/")
	instanceID := providerIDSplit[len(providerIDSplit)-1]
	instanceAttribute, err := env.EC2API.DescribeInstanceAttribute(&ec2.DescribeInstanceAttributeInput{
		InstanceId: awssdk.String(instanceID),
		Attribute:  awssdk.String(attribute),
	})
	Expect(err).ToNot(HaveOccurred())
	return instanceAttribute
}

func EventuallyExpectAMIsToExist(nodeClass *providerv1.EC2NodeClass) *providerv1.EC2NodeClass {
	nc := &providerv1.EC2NodeClass{}
	Eventually(func(g Gomega) {
		g.Expect(env.Client.Get(env, client.ObjectKeyFromObject(nodeClass), nc)).To(Succeed())
		g.Expect(nc.Status.AMIs).ToNot(BeNil())
	}).WithTimeout(30 * time.Second).Should(Succeed())
	return nc
}
