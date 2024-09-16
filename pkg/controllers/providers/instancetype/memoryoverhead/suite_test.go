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

package memoryoverhead_test

import (
	"context"
	"fmt"
	controllersmemoryoverhead "github.com/aws/karpenter-provider-aws/pkg/controllers/providers/instancetype/memoryoverhead"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"testing"

	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/karpenter/pkg/test/v1alpha1"

	corev1 "k8s.io/api/core/v1"
	coreoptions "sigs.k8s.io/karpenter/pkg/operator/options"
	coretest "sigs.k8s.io/karpenter/pkg/test"

	"github.com/aws/aws-sdk-go/service/ec2"
	"github.com/samber/lo"

	"github.com/aws/karpenter-provider-aws/pkg/apis"
	v1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1"
	controllersinstancetype "github.com/aws/karpenter-provider-aws/pkg/controllers/providers/instancetype"
	"github.com/aws/karpenter-provider-aws/pkg/operator/options"
	"github.com/aws/karpenter-provider-aws/pkg/test"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	. "sigs.k8s.io/karpenter/pkg/test/expectations"
	. "sigs.k8s.io/karpenter/pkg/utils/testing"
)

var ctx context.Context
var stop context.CancelFunc
var env *coretest.Environment
var awsEnv *test.Environment
var insTypeController *controllersinstancetype.Controller
var memOverheadController *controllersmemoryoverhead.Controller

func TestAWS(t *testing.T) {
	ctx = TestContextWithLogger(t)
	RegisterFailHandler(Fail)
	RunSpecs(t, "MemoryOverhead")
}

var _ = BeforeSuite(func() {
	env = coretest.NewEnvironment(coretest.WithCRDs(apis.CRDs...), coretest.WithCRDs(v1alpha1.CRDs...))
	ctx = coreoptions.ToContext(ctx, coretest.Options())
	ctx = options.ToContext(ctx, test.Options())
	ctx, stop = context.WithCancel(ctx)
	awsEnv = test.NewEnvironment(ctx, env)
	insTypeController = controllersinstancetype.NewController(awsEnv.InstanceTypesProvider)
	memOverheadController = controllersmemoryoverhead.NewController(env.Client, awsEnv.InstanceTypesProvider)
})

var _ = AfterSuite(func() {
	stop()
	Expect(env.Stop()).To(Succeed(), "Failed to stop environment")
})

var _ = BeforeEach(func() {
	ctx = coreoptions.ToContext(ctx, coretest.Options())
	ctx = options.ToContext(ctx, test.Options())

	awsEnv.Reset()
})

var _ = AfterEach(func() {
	ExpectCleanedUp(ctx, env.Client)
})

var _ = Describe("MemoryOverhead", func() {
	It("should update instance type memory overhead based on node capacities", func() {
		actualMemoryCapacity := int64(3840)
		reportedMemoryCapacity := int64(4096)
		node := &corev1.Node{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-node",
				Labels: map[string]string{
					corev1.LabelInstanceTypeStable: "t3.medium",
				},
				Annotations: map[string]string{
					v1.AnnotationEC2NodeClassHash: "node-class-hash",
				},
			},
			Status: corev1.NodeStatus{
				Capacity: corev1.ResourceList{
					corev1.ResourceMemory: resource.MustParse(fmt.Sprintf("%dMi", actualMemoryCapacity)),
				},
			},
		}
		ExpectApplied(ctx, env.Client, node)

		nodeClass := &v1.EC2NodeClass{
			ObjectMeta: metav1.ObjectMeta{
				Name: "test-nodeclass",
				Annotations: map[string]string{
					v1.AnnotationEC2NodeClassHash: "node-class-hash",
				},
			},
			Status: v1.EC2NodeClassStatus{
				AMIs: []v1.AMI{{
					ID: "ami-12345678",
				}},
				Subnets: []v1.Subnet{
					{
						ID:   "subnet-test1",
						Zone: "test-zone-1a",
					},
					{
						ID:   "subnet-test2",
						Zone: "test-zone-1b",
					},
					{
						ID:   "subnet-test3",
						Zone: "test-zone-1c",
					},
				},
			},
		}
		ExpectApplied(ctx, env.Client, nodeClass)

		ec2InstanceTypes := []*ec2.InstanceTypeInfo{
			{
				InstanceType: lo.ToPtr("t3.medium"),
				MemoryInfo: &ec2.MemoryInfo{
					SizeInMiB: lo.ToPtr(reportedMemoryCapacity),
				},
			},
		}
		awsEnv.EC2API.DescribeInstanceTypesOutput.Set(&ec2.DescribeInstanceTypesOutput{
			InstanceTypes: ec2InstanceTypes,
		})

		Expect(awsEnv.InstanceTypesProvider.UpdateInstanceTypes(ctx)).To(Succeed())
		ExpectReconcileSucceeded(ctx, memOverheadController, client.ObjectKey{})

		instanceTypes, err := awsEnv.InstanceTypesProvider.List(ctx, &v1.KubeletConfiguration{}, nodeClass)
		Expect(err).To(BeNil())
		Expect(instanceTypes).To(HaveLen(1))
		Expect(instanceTypes[0].Capacity.Memory().Value() / 1024 / 1024).To(Equal(actualMemoryCapacity))
	})
})
