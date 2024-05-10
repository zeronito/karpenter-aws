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

package securitygroup_test

import (
	"context"
	"sort"
	"sync"
	"testing"

	"github.com/aws/aws-sdk-go/aws"
	"github.com/aws/aws-sdk-go/service/ec2"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/samber/lo"
	. "knative.dev/pkg/logging/testing"

	"github.com/aws/karpenter/pkg/apis"
	"github.com/aws/karpenter/pkg/apis/settings"
	"github.com/aws/karpenter/pkg/apis/v1beta1"
	"github.com/aws/karpenter/pkg/test"

	coresettings "github.com/aws/karpenter-core/pkg/apis/settings"
	"github.com/aws/karpenter-core/pkg/operator/injection"
	"github.com/aws/karpenter-core/pkg/operator/options"
	"github.com/aws/karpenter-core/pkg/operator/scheme"
	coretest "github.com/aws/karpenter-core/pkg/test"
	. "github.com/aws/karpenter-core/pkg/test/expectations"
)

var ctx context.Context
var stop context.CancelFunc
var opts options.Options
var env *coretest.Environment
var awsEnv *test.Environment
var nodeClass *v1beta1.EC2NodeClass

func TestAWS(t *testing.T) {
	ctx = TestContextWithLogger(t)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Provider/AWS")
}

var _ = BeforeSuite(func() {
	env = coretest.NewEnvironment(scheme.Scheme, coretest.WithCRDs(apis.CRDs...))
	ctx = coresettings.ToContext(ctx, coretest.Settings())
	ctx = settings.ToContext(ctx, test.Settings())
	ctx, stop = context.WithCancel(ctx)
	awsEnv = test.NewEnvironment(ctx, env)
})

var _ = AfterSuite(func() {
	stop()
	Expect(env.Stop()).To(Succeed(), "Failed to stop environment")
})

var _ = BeforeEach(func() {
	ctx = injection.WithOptions(ctx, opts)
	ctx = coresettings.ToContext(ctx, coretest.Settings())
	ctx = settings.ToContext(ctx, test.Settings())
	nodeClass = test.EC2NodeClass(v1beta1.EC2NodeClass{
		Spec: v1beta1.EC2NodeClassSpec{
			AMIFamily: aws.String(v1beta1.AMIFamilyAL2),
			SubnetSelectorTerms: []v1beta1.SubnetSelectorTerm{
				{
					Tags: map[string]string{
						"*": "*",
					},
				},
			},
			SecurityGroupSelectorTerms: []v1beta1.SecurityGroupSelectorTerm{
				{
					Tags: map[string]string{
						"*": "*",
					},
				},
			},
		},
	})
	awsEnv.Reset()
})

var _ = AfterEach(func() {
	ExpectCleanedUp(ctx, env.Client)
})

var _ = Describe("SecurityGroupProvider", func() {
	It("should default to the clusters security groups", func() {
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeClass)
		Expect(err).To(BeNil())
		ExpectConsistsOfSecurityGroups([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("sg-test1"),
				GroupName: aws.String("securityGroup-test1"),
			},
			{
				GroupId:   aws.String("sg-test2"),
				GroupName: aws.String("securityGroup-test2"),
			},
			{
				GroupId:   aws.String("sg-test3"),
				GroupName: aws.String("securityGroup-test3"),
			},
		}, securityGroups)
	})
	It("should discover security groups by tag", func() {
		awsEnv.EC2API.DescribeSecurityGroupsOutput.Set(&ec2.DescribeSecurityGroupsOutput{SecurityGroups: []*ec2.SecurityGroup{
			{GroupName: aws.String("test-sgName-1"), GroupId: aws.String("test-sg-1"), Tags: []*ec2.Tag{{Key: aws.String("kubernetes.io/cluster/test-cluster"), Value: aws.String("test-sg-1")}}},
			{GroupName: aws.String("test-sgName-2"), GroupId: aws.String("test-sg-2"), Tags: []*ec2.Tag{{Key: aws.String("kubernetes.io/cluster/test-cluster"), Value: aws.String("test-sg-2")}}},
		}})
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeClass)
		Expect(err).To(BeNil())
		ExpectConsistsOfSecurityGroups([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("test-sg-1"),
				GroupName: aws.String("test-sgName-1"),
			},
			{
				GroupId:   aws.String("test-sg-2"),
				GroupName: aws.String("test-sgName-2"),
			},
		}, securityGroups)
	})
	It("should discover security groups by multiple tag values", func() {
		nodeClass.Spec.SecurityGroupSelectorTerms = []v1beta1.SecurityGroupSelectorTerm{
			{
				Tags: map[string]string{"Name": "test-security-group-1"},
			},
			{
				Tags: map[string]string{"Name": "test-security-group-2"},
			},
		}
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeClass)
		Expect(err).To(BeNil())
		ExpectConsistsOfSecurityGroups([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("sg-test1"),
				GroupName: aws.String("securityGroup-test1"),
			},
			{
				GroupId:   aws.String("sg-test2"),
				GroupName: aws.String("securityGroup-test2"),
			},
		}, securityGroups)
	})
	It("should discover security groups by ID", func() {
		nodeClass.Spec.SecurityGroupSelectorTerms = []v1beta1.SecurityGroupSelectorTerm{
			{
				ID: "sg-test1",
			},
		}
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeClass)
		Expect(err).To(BeNil())
		ExpectConsistsOfSecurityGroups([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("sg-test1"),
				GroupName: aws.String("securityGroup-test1"),
			},
		}, securityGroups)
	})
	It("should discover security groups by IDs", func() {
		nodeClass.Spec.SecurityGroupSelectorTerms = []v1beta1.SecurityGroupSelectorTerm{
			{
				ID: "sg-test1",
			},
			{
				ID: "sg-test2",
			},
		}
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeClass)
		Expect(err).To(BeNil())
		ExpectConsistsOfSecurityGroups([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("sg-test1"),
				GroupName: aws.String("securityGroup-test1"),
			},
			{
				GroupId:   aws.String("sg-test2"),
				GroupName: aws.String("securityGroup-test2"),
			},
		}, securityGroups)
	})
	It("should discover security groups by IDs and tags", func() {
		nodeClass.Spec.SecurityGroupSelectorTerms = []v1beta1.SecurityGroupSelectorTerm{
			{
				ID:   "sg-test1",
				Tags: map[string]string{"foo": "bar"},
			},
			{
				ID:   "sg-test2",
				Tags: map[string]string{"foo": "bar"},
			},
		}
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeClass)
		Expect(err).To(BeNil())
		ExpectConsistsOfSecurityGroups([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("sg-test1"),
				GroupName: aws.String("securityGroup-test1"),
			},
			{
				GroupId:   aws.String("sg-test2"),
				GroupName: aws.String("securityGroup-test2"),
			},
		}, securityGroups)
	})
	It("should discover security groups by IDs intersected with tags", func() {
		nodeClass.Spec.SecurityGroupSelectorTerms = []v1beta1.SecurityGroupSelectorTerm{
			{
				ID:   "sg-test2",
				Tags: map[string]string{"foo": "bar"},
			},
		}
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeClass)
		Expect(err).To(BeNil())
		ExpectConsistsOfSecurityGroups([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("sg-test2"),
				GroupName: aws.String("securityGroup-test2"),
			},
		}, securityGroups)
	})
	It("should discover security groups by names", func() {
		nodeClass.Spec.SecurityGroupSelectorTerms = []v1beta1.SecurityGroupSelectorTerm{
			{
				Name: "securityGroup-test2",
			},
			{
				Name: "securityGroup-test3",
			},
		}
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeClass)
		Expect(err).To(BeNil())
		ExpectConsistsOfSecurityGroups([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("sg-test2"),
				GroupName: aws.String("securityGroup-test2"),
			},
			{
				GroupId:   aws.String("sg-test3"),
				GroupName: aws.String("securityGroup-test3"),
			},
		}, securityGroups)
	})
	It("should discover security groups by names intersected with tags", func() {
		nodeClass.Spec.SecurityGroupSelectorTerms = []v1beta1.SecurityGroupSelectorTerm{
			{
				Name: "securityGroup-test3",
				Tags: map[string]string{"TestTag": "*"},
			},
		}
		securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeClass)
		Expect(err).To(BeNil())
		ExpectConsistsOfSecurityGroups([]*ec2.SecurityGroup{
			{
				GroupId:   aws.String("sg-test3"),
				GroupName: aws.String("securityGroup-test3"),
			},
		}, securityGroups)
	})
	It("should not cause data races when calling List() simultaneously", func() {
		wg := sync.WaitGroup{}
		for i := 0; i < 10000; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer GinkgoRecover()
				securityGroups, err := awsEnv.SecurityGroupProvider.List(ctx, nodeClass)
				Expect(err).ToNot(HaveOccurred())

				Expect(securityGroups).To(HaveLen(3))
				// Sort everything in parallel and ensure that we don't get data races
				sort.Slice(securityGroups, func(i, j int) bool {
					return *securityGroups[i].GroupId < *securityGroups[j].GroupId
				})
				Expect(securityGroups).To(BeEquivalentTo([]*ec2.SecurityGroup{
					{
						GroupId:   lo.ToPtr("sg-test1"),
						GroupName: lo.ToPtr("securityGroup-test1"),
						Tags: []*ec2.Tag{
							{
								Key:   lo.ToPtr("Name"),
								Value: lo.ToPtr("test-security-group-1"),
							},
							{
								Key:   lo.ToPtr("foo"),
								Value: lo.ToPtr("bar"),
							},
						},
					},
					{
						GroupId:   lo.ToPtr("sg-test2"),
						GroupName: lo.ToPtr("securityGroup-test2"),
						Tags: []*ec2.Tag{
							{
								Key:   lo.ToPtr("Name"),
								Value: lo.ToPtr("test-security-group-2"),
							},
							{
								Key:   lo.ToPtr("foo"),
								Value: lo.ToPtr("bar"),
							},
						},
					},
					{
						GroupId:   lo.ToPtr("sg-test3"),
						GroupName: lo.ToPtr("securityGroup-test3"),
						Tags: []*ec2.Tag{
							{
								Key:   lo.ToPtr("Name"),
								Value: lo.ToPtr("test-security-group-3"),
							},
							{
								Key: lo.ToPtr("TestTag"),
							},
							{
								Key:   lo.ToPtr("foo"),
								Value: lo.ToPtr("bar"),
							},
						},
					},
				}))
			}()
		}
		wg.Wait()
	})
})

func ExpectConsistsOfSecurityGroups(expected, actual []*ec2.SecurityGroup) {
	GinkgoHelper()
	Expect(actual).To(HaveLen(len(expected)))
	for _, elem := range expected {
		_, ok := lo.Find(actual, func(s *ec2.SecurityGroup) bool {
			return lo.FromPtr(s.GroupId) == lo.FromPtr(elem.GroupId) &&
				lo.FromPtr(s.GroupName) == lo.FromPtr(elem.GroupName)
		})
		Expect(ok).To(BeTrue(), `Expected security group with {"GroupId": %q, "GroupName": %q} to exist`, lo.FromPtr(elem.GroupId), lo.FromPtr(elem.GroupName))
	}
}
