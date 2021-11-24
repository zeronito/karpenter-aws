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

package scheduling_test

import (
	"context"
	"testing"

	"github.com/aws/karpenter/pkg/apis/provisioning/v1alpha5"
	"github.com/aws/karpenter/pkg/cloudprovider/fake"
	"github.com/aws/karpenter/pkg/cloudprovider/registry"
	"github.com/aws/karpenter/pkg/controllers/provisioning"
	"github.com/aws/karpenter/pkg/controllers/scheduling"
	"github.com/aws/karpenter/pkg/test"
	"sigs.k8s.io/controller-runtime/pkg/client"

	v1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	corev1 "k8s.io/client-go/kubernetes/typed/core/v1"

	. "github.com/aws/karpenter/pkg/test/expectations"
	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	. "knative.dev/pkg/logging/testing"
)

var ctx context.Context
var provisioner *v1alpha5.Provisioner
var controller *provisioning.Controller
var scheduler *scheduling.Controller
var env *test.Environment

func TestAPIs(t *testing.T) {
	ctx = TestContextWithLogger(t)
	RegisterFailHandler(Fail)
	RunSpecs(t, "Controllers/Allocation/Scheduling")
}

var _ = BeforeSuite(func() {
	env = test.NewEnvironment(ctx, func(e *test.Environment) {
		cloudProvider := &fake.CloudProvider{}
		registry.RegisterOrDie(ctx, cloudProvider)
		controller = provisioning.NewController(ctx, e.Client, corev1.NewForConfigOrDie(e.Config), cloudProvider)
		scheduler = scheduling.NewController(e.Client, controller)
	})
	Expect(env.Start()).To(Succeed(), "Failed to start environment")
})

var _ = AfterSuite(func() {
	Expect(env.Stop()).To(Succeed(), "Failed to stop environment")
})

var _ = BeforeEach(func() {
	provisioner = &v1alpha5.Provisioner{
		ObjectMeta: metav1.ObjectMeta{Name: v1alpha5.DefaultProvisioner.Name},
		Spec:       v1alpha5.ProvisionerSpec{},
	}
	provisioner.SetDefaults(ctx)
})

var _ = AfterEach(func() {
	ExpectCleanedUp(env.Client)
})

var _ = Describe("Combined Constraints", func() {
	Context("Custom Labels", func() {
		It("should schedule unconstrained pods that don't have matching node selectors", func() {
			provisioner.Spec.Labels = map[string]string{"test-key": "test-value"}
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod())[0]
			node := ExpectScheduled(ctx, env.Client, pod)
			Expect(node.Labels).To(HaveKeyWithValue("test-key", "test-value"))
		})
		It("should not schedule pods that have conflicting node selectors", func() {
			provisioner.Spec.Labels = map[string]string{"test-key": "test-value"}
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{NodeSelector: map[string]string{"test-key": "different-value"}},
			))[0]
			ExpectNotScheduled(ctx, env.Client, pod)
		})
		It("should schedule pods that have matching requirements", func() {
			provisioner.Spec.Labels = map[string]string{"test-key": "test-value"}
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{NodeRequirements: []v1.NodeSelectorRequirement{
					{Key: "test-key", Operator: v1.NodeSelectorOpIn, Values: []string{"test-value", "another-value"}},
				}},
			))[0]
			node := ExpectScheduled(ctx, env.Client, pod)
			Expect(node.Labels).To(HaveKeyWithValue("test-key", "test-value"))
		})
		It("should not schedule pods that have conflicting requirements", func() {
			provisioner.Spec.Labels = map[string]string{"test-key": "test-value"}
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{NodeRequirements: []v1.NodeSelectorRequirement{
					{Key: "test-key", Operator: v1.NodeSelectorOpIn, Values: []string{"another-value"}},
				}},
			))[0]
			ExpectNotScheduled(ctx, env.Client, pod)
		})
		It("should schedule pods that have matching preferences", func() {
			provisioner.Spec.Labels = map[string]string{"test-key": "test-value"}
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{NodePreferences: []v1.NodeSelectorRequirement{
					{Key: "test-key", Operator: v1.NodeSelectorOpIn, Values: []string{"another-value", "test-value"}},
				}},
			))[0]
			node := ExpectScheduled(ctx, env.Client, pod)
			Expect(node.Labels).To(HaveKeyWithValue("test-key", "test-value"))
		})
		It("should not schedule pods with have conflicting preferences", func() {
			provisioner.Spec.Labels = map[string]string{"test-key": "test-value"}
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{NodePreferences: []v1.NodeSelectorRequirement{
					{Key: "test-key", Operator: v1.NodeSelectorOpNotIn, Values: []string{"test-value"}},
				}},
			))[0]
			ExpectNotScheduled(ctx, env.Client, pod)
		})
	})
	Context("Well Known Labels", func() {
		It("should use provisioner constraints", func() {
			provisioner.Spec.Requirements = v1alpha5.Requirements{{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-2"}}}
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod())[0]
			node := ExpectScheduled(ctx, env.Client, pod)
			Expect(node.Labels).To(HaveKeyWithValue(v1.LabelTopologyZone, "test-zone-2"))
		})
		It("should use node selectors", func() {
			provisioner.Spec.Requirements = v1alpha5.Requirements{{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1", "test-zone-2"}}}
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{NodeSelector: map[string]string{v1.LabelTopologyZone: "test-zone-2"}},
			))[0]
			node := ExpectScheduled(ctx, env.Client, pod)
			Expect(node.Labels).To(HaveKeyWithValue(v1.LabelTopologyZone, "test-zone-2"))
		})
		It("should not schedule the pod if nodeselector unknown", func() {
			provisioner.Spec.Requirements = v1alpha5.Requirements{{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1"}}}
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{NodeSelector: map[string]string{v1.LabelTopologyZone: "unknown"}},
			))[0]
			ExpectNotScheduled(ctx, env.Client, pod)
		})
		It("should not schedule if node selector outside of provisioner constraints", func() {
			provisioner.Spec.Requirements = v1alpha5.Requirements{{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1"}}}
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{NodeSelector: map[string]string{v1.LabelTopologyZone: "test-zone-2"}},
			))[0]
			ExpectNotScheduled(ctx, env.Client, pod)
		})
		It("should schedule compatible requirements with Operator=In", func() {
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{NodeRequirements: []v1.NodeSelectorRequirement{
					{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-3"}},
				}},
			))[0]
			node := ExpectScheduled(ctx, env.Client, pod)
			Expect(node.Labels).To(HaveKeyWithValue(v1.LabelTopologyZone, "test-zone-3"))
		})
		It("should not schedule incompatible preferences and requirements with Operator=In", func() {
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{NodeRequirements: []v1.NodeSelectorRequirement{
					{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"unknown"}},
				}},
			))[0]
			ExpectNotScheduled(ctx, env.Client, pod)
		})
		It("should schedule compatible requirements with Operator=NotIn", func() {
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{NodeRequirements: []v1.NodeSelectorRequirement{
					{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpNotIn, Values: []string{"test-zone-1", "test-zone-2", "unknown"}},
				}},
			))[0]
			node := ExpectScheduled(ctx, env.Client, pod)
			Expect(node.Labels).To(HaveKeyWithValue(v1.LabelTopologyZone, "test-zone-3"))
		})
		It("should not schedule incompatible preferences and requirements with Operator=NotIn", func() {
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{
					NodeRequirements: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpNotIn, Values: []string{"test-zone-1", "test-zone-2", "test-zone-3", "unknown"}},
					}},
			))[0]
			ExpectNotScheduled(ctx, env.Client, pod)
		})
		It("should schedule compatible preferences and requirements with Operator=In", func() {
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{
					NodeRequirements: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1", "test-zone-2", "test-zone-3", "unknown"}}},
					NodePreferences: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-2", "unknown"}}},
				},
			))[0]
			node := ExpectScheduled(ctx, env.Client, pod)
			Expect(node.Labels).To(HaveKeyWithValue(v1.LabelTopologyZone, "test-zone-2"))
		})
		It("should not schedule incompatible preferences and requirements with Operator=In", func() {
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{
					NodeRequirements: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1", "test-zone-2", "test-zone-3", "unknown"}}},
					NodePreferences: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"unknown"}}},
				},
			))[0]
			ExpectNotScheduled(ctx, env.Client, pod)
		})

		It("should schedule compatible preferences and requirements with Operator=NotIn", func() {
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{
					NodeRequirements: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1", "test-zone-2", "test-zone-3", "unknown"}}},
					NodePreferences: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpNotIn, Values: []string{"test-zone-1", "test-zone-3"}}},
				},
			))[0]
			node := ExpectScheduled(ctx, env.Client, pod)
			Expect(node.Labels).To(HaveKeyWithValue(v1.LabelTopologyZone, "test-zone-2"))
		})
		It("should not schedule incompatible preferences and requirements with Operator=NotIn", func() {
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{
					NodeRequirements: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1", "test-zone-2", "test-zone-3", "unknown"}}},
					NodePreferences: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpNotIn, Values: []string{"test-zone-1", "test-zone-2", "test-zone-3"}}},
				},
			))[0]
			ExpectNotScheduled(ctx, env.Client, pod)
		})
		It("should schedule compatible node selectors, preferences and requirements", func() {
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{
					NodeSelector: map[string]string{v1.LabelTopologyZone: "test-zone-3"},
					NodeRequirements: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1", "test-zone-2", "test-zone-3"}}},
					NodePreferences: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1", "test-zone-2", "test-zone-3"}}},
				},
			))[0]
			node := ExpectScheduled(ctx, env.Client, pod)
			Expect(node.Labels).To(HaveKeyWithValue(v1.LabelTopologyZone, "test-zone-3"))
		})
		It("should not schedule incompatible node selectors, preferences and requirements", func() {
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{
					NodeSelector: map[string]string{v1.LabelTopologyZone: "test-zone-3"},
					NodeRequirements: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1", "test-zone-3"}}},
					NodePreferences: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpNotIn, Values: []string{"test-zone-2", "test-zone-3"}}},
				},
			))[0]
			ExpectNotScheduled(ctx, env.Client, pod)
		})
		It("should combine multidimensional node selectors, preferences and requirements", func() {
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{
					NodeSelector: map[string]string{
						v1.LabelTopologyZone:       "test-zone-3",
						v1.LabelInstanceTypeStable: "arm-instance-type",
					},
					NodeRequirements: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1", "test-zone-3"}},
						{Key: v1.LabelInstanceTypeStable, Operator: v1.NodeSelectorOpIn, Values: []string{"default-instance-type", "arm-instance-type"}},
					},
					NodePreferences: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpNotIn, Values: []string{"unnknown"}},
						{Key: v1.LabelInstanceTypeStable, Operator: v1.NodeSelectorOpNotIn, Values: []string{"unknown"}},
					},
				},
			))[0]
			node := ExpectScheduled(ctx, env.Client, pod)
			Expect(node.Labels).To(HaveKeyWithValue(v1.LabelTopologyZone, "test-zone-3"))
			Expect(node.Labels).To(HaveKeyWithValue(v1.LabelInstanceTypeStable, "arm-instance-type"))
		})
		It("should not combine incompatible multidimensional node selectors, preferences and requirements", func() {
			pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
				test.PodOptions{
					NodeSelector: map[string]string{
						v1.LabelTopologyZone:       "test-zone-3",
						v1.LabelInstanceTypeStable: "arm-instance-type",
					},
					NodeRequirements: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1", "test-zone-3"}},
						{Key: v1.LabelInstanceTypeStable, Operator: v1.NodeSelectorOpIn, Values: []string{"default-instance-type", "arm-instance-type"}},
					},
					NodePreferences: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpNotIn, Values: []string{"test-zone-3"}},
						{Key: v1.LabelInstanceTypeStable, Operator: v1.NodeSelectorOpNotIn, Values: []string{"arm-instance-type"}},
					},
				},
			))[0]
			ExpectNotScheduled(ctx, env.Client, pod)
		})
	})
})

var _ = Describe("Preferential Fallback", func() {
	Context("Required", func() {
		It("should not relax the final term", func() {
			provisioner.Spec.Requirements = v1alpha5.Requirements{
				{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1"}},
				{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"default-instance-type"}},
			}
			pod := test.UnschedulablePod()
			pod.Spec.Affinity = &v1.Affinity{NodeAffinity: &v1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{NodeSelectorTerms: []v1.NodeSelectorTerm{
				{MatchExpressions: []v1.NodeSelectorRequirement{
					{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"invalid"}}, // Should not be relaxed
				}},
			}}}}
			// Don't relax
			pod = ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, pod)[0]
			ExpectNotScheduled(ctx, env.Client, pod)

			// Don't relax
			pod = ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, pod)[0]
			ExpectNotScheduled(ctx, env.Client, pod)
		})
		It("should relax multiple terms", func() {
			pod := test.UnschedulablePod()
			pod.Spec.Affinity = &v1.Affinity{NodeAffinity: &v1.NodeAffinity{RequiredDuringSchedulingIgnoredDuringExecution: &v1.NodeSelector{NodeSelectorTerms: []v1.NodeSelectorTerm{
				{MatchExpressions: []v1.NodeSelectorRequirement{
					{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"invalid"}},
				}},
				{MatchExpressions: []v1.NodeSelectorRequirement{
					{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"invalid"}},
				}},
				{MatchExpressions: []v1.NodeSelectorRequirement{
					{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1"}},
				}},
				{MatchExpressions: []v1.NodeSelectorRequirement{
					{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-2"}}, // OR operator, never get to this one
				}},
			}}}}
			// Remove first term
			pod = ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, pod)[0]
			ExpectNotScheduled(ctx, env.Client, pod)
			// Remove second term
			pod = ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, pod)[0]
			ExpectNotScheduled(ctx, env.Client, pod)
			// Success
			pod = ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, pod)[0]
			node := ExpectScheduled(ctx, env.Client, pod)
			Expect(node.Labels).To(HaveKeyWithValue(v1.LabelTopologyZone, "test-zone-1"))
		})
	})
	Context("Preferred", func() {
		It("should relax all terms", func() {
			pod := test.UnschedulablePod()
			pod.Spec.Affinity = &v1.Affinity{NodeAffinity: &v1.NodeAffinity{PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
				{
					Weight: 1, Preference: v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"invalid"}},
					}},
				},
				{
					Weight: 1, Preference: v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{
						{Key: v1.LabelInstanceTypeStable, Operator: v1.NodeSelectorOpIn, Values: []string{"invalid"}},
					}},
				},
			}}}
			// Remove first term
			pod = ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, pod)[0]
			ExpectNotScheduled(ctx, env.Client, pod)
			// Remove second term
			pod = ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, pod)[0]
			ExpectNotScheduled(ctx, env.Client, pod)
			// Success
			pod = ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, pod)[0]
			ExpectScheduled(ctx, env.Client, pod)
		})
		It("should relax to use lighter weights", func() {
			provisioner.Spec.Requirements = v1alpha5.Requirements{{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1", "test-zone-2"}}}
			pod := test.UnschedulablePod()
			pod.Spec.Affinity = &v1.Affinity{NodeAffinity: &v1.NodeAffinity{PreferredDuringSchedulingIgnoredDuringExecution: []v1.PreferredSchedulingTerm{
				{
					Weight: 100, Preference: v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{
						{Key: v1.LabelInstanceTypeStable, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-3"}},
					}},
				},
				{
					Weight: 50, Preference: v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-2"}},
					}},
				},
				{
					Weight: 1, Preference: v1.NodeSelectorTerm{MatchExpressions: []v1.NodeSelectorRequirement{ // OR operator, never get to this one
						{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1"}},
					}},
				},
			}}}
			// Remove heaviest term
			pod = ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, pod)[0]
			ExpectNotScheduled(ctx, env.Client, pod)
			// Success
			pod = ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, pod)[0]
			node := ExpectScheduled(ctx, env.Client, pod)
			Expect(node.Labels).To(HaveKeyWithValue(v1.LabelTopologyZone, "test-zone-2"))
		})
	})
})

var _ = Describe("Topology", func() {
	labels := map[string]string{"test": "test"}

	It("should ignore unknown topology keys", func() {
		pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
			test.PodOptions{Labels: labels, TopologySpreadConstraints: []v1.TopologySpreadConstraint{{
				TopologyKey:       "unknown",
				WhenUnsatisfiable: v1.DoNotSchedule,
				LabelSelector:     &metav1.LabelSelector{MatchLabels: labels},
				MaxSkew:           1,
			}}},
		))[0]
		ExpectNotScheduled(ctx, env.Client, pod)
	})

	Context("Zonal", func() {
		It("should balance pods across zones", func() {
			topology := []v1.TopologySpreadConstraint{{
				TopologyKey:       v1.LabelTopologyZone,
				WhenUnsatisfiable: v1.DoNotSchedule,
				LabelSelector:     &metav1.LabelSelector{MatchLabels: labels},
				MaxSkew:           1,
			}}
			ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
			)
			ExpectSkew(env.Client, v1.LabelTopologyZone).To(ConsistOf(1, 1, 2))
		})
		It("should respect provisioner zonal constraints", func() {
			provisioner.Spec.Requirements = v1alpha5.Requirements{{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{"test-zone-1", "test-zone-2"}}}
			topology := []v1.TopologySpreadConstraint{{
				TopologyKey:       v1.LabelTopologyZone,
				WhenUnsatisfiable: v1.DoNotSchedule,
				LabelSelector:     &metav1.LabelSelector{MatchLabels: labels},
				MaxSkew:           1,
			}}
			ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
			)
			ExpectSkew(env.Client, v1.LabelTopologyZone).To(ConsistOf(2, 2))
		})
		It("should only count scheduled pods with matching labels scheduled to nodes with a corresponding domain", func() {
			firstNode := test.Node(test.NodeOptions{Labels: map[string]string{v1.LabelTopologyZone: "test-zone-1"}})
			secondNode := test.Node(test.NodeOptions{Labels: map[string]string{v1.LabelTopologyZone: "test-zone-2"}})
			thirdNode := test.Node(test.NodeOptions{}) // missing topology domain
			ExpectCreated(env.Client, provisioner, firstNode, secondNode, thirdNode)
			topology := []v1.TopologySpreadConstraint{{
				TopologyKey:       v1.LabelTopologyZone,
				WhenUnsatisfiable: v1.DoNotSchedule,
				LabelSelector:     &metav1.LabelSelector{MatchLabels: labels},
				MaxSkew:           1,
			}}
			ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
				test.Pod(test.PodOptions{Labels: labels}), // ignored, pending
				test.Pod(test.PodOptions{}),               // ignored, missing labels
				test.Pod(test.PodOptions{Labels: labels, NodeName: firstNode.Name}),
				test.Pod(test.PodOptions{Labels: labels, NodeName: firstNode.Name}),
				test.Pod(test.PodOptions{Labels: labels, NodeName: secondNode.Name}),
				test.Pod(test.PodOptions{Labels: labels, NodeName: thirdNode.Name}), //ignored, no domain
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
			)
			nodes := v1.NodeList{}
			Expect(env.Client.List(ctx, &nodes)).To(Succeed())
			ExpectSkew(env.Client, v1.LabelTopologyZone).To(ConsistOf(2, 2, 1))
		})
	})

	Context("Hostname", func() {
		It("should balance pods across nodes", func() {
			topology := []v1.TopologySpreadConstraint{{
				TopologyKey:       v1.LabelHostname,
				WhenUnsatisfiable: v1.DoNotSchedule,
				LabelSelector:     &metav1.LabelSelector{MatchLabels: labels},
				MaxSkew:           1,
			}}
			ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
			)
			ExpectSkew(env.Client, v1.LabelHostname).To(ConsistOf(1, 1, 1, 1))
		})
		It("should balance pods on the same hostname up to maxskew", func() {
			topology := []v1.TopologySpreadConstraint{{
				TopologyKey:       v1.LabelHostname,
				WhenUnsatisfiable: v1.DoNotSchedule,
				LabelSelector:     &metav1.LabelSelector{MatchLabels: labels},
				MaxSkew:           4,
			}}
			ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
				test.UnschedulablePod(test.PodOptions{Labels: labels, TopologySpreadConstraints: topology}),
			)
			ExpectSkew(env.Client, v1.LabelHostname).To(ConsistOf(4))
		})
	})

	Context("Combined Hostname and Zonal Topology", func() {
		It("should spread pods while respecting both constraints", func() {
			topology := []v1.TopologySpreadConstraint{{
				TopologyKey:       v1.LabelTopologyZone,
				WhenUnsatisfiable: v1.DoNotSchedule,
				LabelSelector:     &metav1.LabelSelector{MatchLabels: labels},
				MaxSkew:           1,
			}, {
				TopologyKey:       v1.LabelHostname,
				WhenUnsatisfiable: v1.DoNotSchedule,
				LabelSelector:     &metav1.LabelSelector{MatchLabels: labels},
				MaxSkew:           3,
			}}
			ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
				MakePods(2, test.PodOptions{Labels: labels, TopologySpreadConstraints: topology})...,
			)
			ExpectSkew(env.Client, v1.LabelTopologyZone).To(ConsistOf(1, 1))
			ExpectSkew(env.Client, v1.LabelHostname).ToNot(ContainElements(BeNumerically(">", 3)))

			ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
				MakePods(3, test.PodOptions{Labels: labels, TopologySpreadConstraints: topology})...,
			)
			ExpectSkew(env.Client, v1.LabelTopologyZone).To(ConsistOf(2, 2, 1))
			ExpectSkew(env.Client, v1.LabelHostname).ToNot(ContainElements(BeNumerically(">", 3)))

			ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
				MakePods(5, test.PodOptions{Labels: labels, TopologySpreadConstraints: topology})...,
			)
			ExpectSkew(env.Client, v1.LabelTopologyZone).To(ConsistOf(4, 3, 3))
			ExpectSkew(env.Client, v1.LabelHostname).ToNot(ContainElements(BeNumerically(">", 3)))

			ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
				MakePods(11, test.PodOptions{Labels: labels, TopologySpreadConstraints: topology})...,
			)
			ExpectSkew(env.Client, v1.LabelTopologyZone).To(ConsistOf(7, 7, 7))
			ExpectSkew(env.Client, v1.LabelHostname).ToNot(ContainElements(BeNumerically(">", 3)))
		})
	})

	// https://kubernetes.io/docs/concepts/workloads/pods/pod-topology-spread-constraints/#interaction-with-node-affinity-and-node-selectors
	Context("Combined Zonal Topology and Affinity", func() {
		It("should limit spread options by nodeSelector", func() {
			topology := []v1.TopologySpreadConstraint{{
				TopologyKey:       v1.LabelTopologyZone,
				WhenUnsatisfiable: v1.DoNotSchedule,
				LabelSelector:     &metav1.LabelSelector{MatchLabels: labels},
				MaxSkew:           1,
			}}
			ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
				append(
					MakePods(5, test.PodOptions{
						Labels:                    labels,
						TopologySpreadConstraints: topology,
						NodeSelector:              map[string]string{v1.LabelTopologyZone: "test-zone-1"},
					}),
					MakePods(5, test.PodOptions{
						Labels:                    labels,
						TopologySpreadConstraints: topology,
						NodeSelector:              map[string]string{v1.LabelTopologyZone: "test-zone-2"},
					})...,
				)...,
			)
			ExpectSkew(env.Client, v1.LabelTopologyZone).To(ConsistOf(5, 5))
		})
		It("should limit spread options by node affinity", func() {
			topology := []v1.TopologySpreadConstraint{{
				TopologyKey:       v1.LabelTopologyZone,
				WhenUnsatisfiable: v1.DoNotSchedule,
				LabelSelector:     &metav1.LabelSelector{MatchLabels: labels},
				MaxSkew:           1,
			}}
			ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, append(
				MakePods(6, test.PodOptions{
					Labels:                    labels,
					TopologySpreadConstraints: topology,
					NodeRequirements: []v1.NodeSelectorRequirement{{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpIn, Values: []string{
						"test-zone-1", "test-zone-2",
					}}},
				}),
				MakePods(1, test.PodOptions{
					Labels:                    labels,
					TopologySpreadConstraints: topology,
					NodeRequirements: []v1.NodeSelectorRequirement{{Key: v1.LabelTopologyZone, Operator: v1.NodeSelectorOpNotIn, Values: []string{
						"test-zone-2", "test-zone-3",
					}}},
				})...,
			)...)
			ExpectSkew(env.Client, v1.LabelTopologyZone).To(ConsistOf(4, 3))
			ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
				MakePods(5, test.PodOptions{
					Labels:                    labels,
					TopologySpreadConstraints: topology,
				})...,
			)
			ExpectSkew(env.Client, v1.LabelTopologyZone).To(ConsistOf(4, 4, 4))
		})
	})
})

var _ = Describe("Taints", func() {
	It("should taint nodes with provisioner taints", func() {
		provisioner.Spec.Taints = []v1.Taint{{Key: "test", Value: "bar", Effect: v1.TaintEffectNoSchedule}}
		pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod(
			test.PodOptions{Tolerations: []v1.Toleration{{Effect: v1.TaintEffectNoSchedule, Operator: v1.TolerationOpExists}}},
		))[0]
		node := ExpectScheduled(ctx, env.Client, pod)
		Expect(node.Spec.Taints).To(ContainElement(provisioner.Spec.Taints[0]))
	})
	It("should schedule pods that tolerate provisioner constraints", func() {
		provisioner.Spec.Taints = []v1.Taint{{Key: "test-key", Value: "test-value", Effect: v1.TaintEffectNoSchedule}}
		for _, pod := range ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
			// Tolerates with OpExists
			test.UnschedulablePod(test.PodOptions{Tolerations: []v1.Toleration{{Key: "test-key", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoSchedule}}}),
			// Tolerates with OpEqual
			test.UnschedulablePod(test.PodOptions{Tolerations: []v1.Toleration{{Key: "test-key", Value: "test-value", Operator: v1.TolerationOpEqual, Effect: v1.TaintEffectNoSchedule}}}),
		) {
			ExpectScheduled(ctx, env.Client, pod)
		}
		for _, pod := range ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
			// Missing toleration
			test.UnschedulablePod(),
			// key mismatch with OpExists
			test.UnschedulablePod(test.PodOptions{Tolerations: []v1.Toleration{{Key: "invalid", Operator: v1.TolerationOpExists}}}),
			// value mismatch
			test.UnschedulablePod(test.PodOptions{Tolerations: []v1.Toleration{{Key: "test-key", Operator: v1.TolerationOpEqual, Effect: v1.TaintEffectNoSchedule}}}),
		) {
			ExpectNotScheduled(ctx, env.Client, pod)
		}
	})
	It("should not generate taints for OpExists", func() {
		pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
			test.UnschedulablePod(test.PodOptions{Tolerations: []v1.Toleration{{Key: "test-key", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoExecute}}}),
		)[0]
		node := ExpectScheduled(ctx, env.Client, pod)
		Expect(node.Spec.Taints).To(HaveLen(2)) // Expect no taints generated beyond defaults
	})
	It("should generate taints for pod tolerations", func() {
		Skip("until taint generation is reimplmented")
		pods := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
			// Matching pods schedule together on a node with a matching taint
			test.UnschedulablePod(test.PodOptions{Tolerations: []v1.Toleration{
				{Key: "test-key", Operator: v1.TolerationOpEqual, Value: "test-value", Effect: v1.TaintEffectNoSchedule}},
			}),
			test.UnschedulablePod(test.PodOptions{Tolerations: []v1.Toleration{
				{Key: "test-key", Operator: v1.TolerationOpEqual, Value: "test-value", Effect: v1.TaintEffectNoSchedule}},
			}),
			// Key is different, generate new node with a taint for this key
			test.UnschedulablePod(test.PodOptions{Tolerations: []v1.Toleration{
				{Key: "another-test-key", Operator: v1.TolerationOpEqual, Value: "test-value", Effect: v1.TaintEffectNoSchedule}},
			}),
			// Value is different, generate new node with a taint for this value
			test.UnschedulablePod(test.PodOptions{Tolerations: []v1.Toleration{
				{Key: "test-key", Operator: v1.TolerationOpEqual, Value: "another-test-value", Effect: v1.TaintEffectNoSchedule}},
			}),
			// Effect is different, generate new node with a taint for this value
			test.UnschedulablePod(test.PodOptions{Tolerations: []v1.Toleration{
				{Key: "test-key", Operator: v1.TolerationOpEqual, Value: "test-value", Effect: v1.TaintEffectNoExecute}},
			}),
			// Missing effect, generate a new node with a taints for all effects
			test.UnschedulablePod(test.PodOptions{Tolerations: []v1.Toleration{
				{Key: "test-key", Operator: v1.TolerationOpEqual, Value: "test-value"}},
			}),
			// // No taint generated
			test.UnschedulablePod(test.PodOptions{Tolerations: []v1.Toleration{{Key: "test-key", Operator: v1.TolerationOpExists, Effect: v1.TaintEffectNoExecute}}}),
		)
		for i, expectedTaintsPerNode := range [][]v1.Taint{
			{{Key: "test-key", Value: "test-value", Effect: v1.TaintEffectNoSchedule}},
			{{Key: "test-key", Value: "test-value", Effect: v1.TaintEffectNoSchedule}},
			{{Key: "another-test-key", Value: "test-value", Effect: v1.TaintEffectNoSchedule}},
			{{Key: "test-key", Value: "another-test-value", Effect: v1.TaintEffectNoSchedule}},
			{{Key: "test-key", Value: "test-value", Effect: v1.TaintEffectNoExecute}},
			{{Key: "test-key", Value: "test-value", Effect: v1.TaintEffectNoSchedule}, {Key: "test-key", Value: "test-value", Effect: v1.TaintEffectNoExecute}},
		} {
			node := ExpectScheduled(ctx, env.Client, pods[i])
			for _, taint := range expectedTaintsPerNode {
				Expect(node.Spec.Taints).To(ContainElement(taint))
			}
		}
		node := ExpectScheduled(ctx, env.Client, pods[len(pods)-1])
		Expect(node.Spec.Taints).To(HaveLen(2)) // Expect no taints generated beyond defaults
	})
})

var _ = Describe("Multiple Provisioners", func() {
	It("should schedule to an explicitly selected provisioner", func() {
		provisioner2 := provisioner.DeepCopy()
		provisioner2.Name = "provisioner2"
		ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner2)
		pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
			test.UnschedulablePod(test.PodOptions{NodeSelector: map[string]string{v1alpha5.ProvisionerNameLabelKey: provisioner2.Name}}),
		)[0]
		node := ExpectScheduled(ctx, env.Client, pod)
		Expect(node.Labels[v1alpha5.ProvisionerNameLabelKey]).To(Equal(provisioner2.Name))
	})
	It("should schedule to a provisioner by labels", func() {
		provisioner2 := provisioner.DeepCopy()
		provisioner2.Name = "provisioner2"
		provisioner2.Spec.Labels = map[string]string{"foo": "bar"}
		provisioner.Spec.Labels = map[string]string{"foo": "baz"}
		ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner2)
		pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner,
			test.UnschedulablePod(test.PodOptions{NodeSelector: map[string]string{"foo": "bar"}}),
		)[0]
		node := ExpectScheduled(ctx, env.Client, pod)
		Expect(node.Labels[v1alpha5.ProvisionerNameLabelKey]).To(Equal(provisioner2.Name))
	})
	It("should prioritize provisioners alphabetically if multiple match", func() {
		provisioner2 := provisioner.DeepCopy()
		provisioner2.Name = "aaaaaaaaa"
		ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner2)
		pod := ExpectProvisioned(ctx, env.Client, scheduler, controller, provisioner, test.UnschedulablePod())[0]
		node := ExpectScheduled(ctx, env.Client, pod)
		Expect(node.Labels[v1alpha5.ProvisionerNameLabelKey]).To(Equal(provisioner2.Name))
	})
})

func MakePods(count int, options test.PodOptions) (pods []*v1.Pod) {
	for i := 0; i < count; i++ {
		pods = append(pods, test.UnschedulablePod(options))
	}
	return pods
}

func ExpectSkew(c client.Client, topologyKey string) Assertion {
	nodes := &v1.NodeList{}
	Expect(c.List(ctx, nodes)).To(Succeed())
	pods := &v1.PodList{}
	Expect(c.List(ctx, pods)).To(Succeed())
	skew := map[string]int{}
	for _, pod := range pods.Items {
		for _, node := range nodes.Items {
			if pod.Spec.NodeName == node.Name {
				if topologyKey == v1.LabelHostname {
					skew[node.Name]++ // Check node name since hostname labels aren't applied
				} else if key, ok := node.Labels[topologyKey]; ok {
					skew[key]++
				}
			}
		}
	}
	return Expect(skew)
}
