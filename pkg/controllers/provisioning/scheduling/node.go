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

package scheduling

import (
	"fmt"
	"strings"

	v1 "k8s.io/api/core/v1"

	"github.com/aws/karpenter/pkg/apis/provisioning/v1alpha5"
	"github.com/aws/karpenter/pkg/cloudprovider"
	"github.com/aws/karpenter/pkg/utils/resources"
)

// Node is a set of constraints, compatible pods, and possible instance types that could fulfill these constraints. This
// will be turned into one or more actual node instances within the cluster after bin packing.
type Node struct {
	Constraints         *Constraints
	InstanceTypeOptions []cloudprovider.InstanceType
	Pods                []*v1.Pod

	requests v1.ResourceList
}

func NewNode(constraints *Constraints, daemonResources v1.ResourceList, instanceTypes []cloudprovider.InstanceType) *Node {
	return &Node{
		Constraints:         constraints.DeepCopy(),
		InstanceTypeOptions: instanceTypes,
		requests:            daemonResources,
	}
}

func (n *Node) Add(pod *v1.Pod) error {
	podRequirements := NewPodRequirements(pod)

	if len(n.Pods) != 0 {
		// TODO: remove this check for n.Pods once we properly support hostname topology spread
		if err := n.Constraints.Requirements.Compatible(podRequirements); err != nil {
			return err
		}
	}
	requirements := *n.Constraints.Requirements.DeepCopy()
	requirements.Add(podRequirements)
	requests := resources.Merge(n.requests, resources.RequestsForPods(pod))
	instanceTypes := FilterInstanceTypes(n.InstanceTypeOptions, requirements, requests)
	if len(instanceTypes) == 0 {
		// We only report a user-friendly error message in the event of the first pod not fitting on the node.  In this
		// case, the node will never be used as the pod is unschedulable given the current set of constraints.  The other
		// case where a pod doesn't fit on a node are caused by compatibility reasons with the current pods on the node.  This
		// occurs much more often and that error message is never shown to the user.  Constructing that error message ends up
		// dominating runtime as it happens very frequently. We improve from scheduling 1200 pods/sec to 1900+ pods/sec with
		// this change using our scheduling benchmark.
		if len(n.Pods) == 0 {
			return fmt.Errorf("no instance type satisfied resources %s and requirements %s", resources.String(resources.RequestsForPods(pod)), n.Constraints.Requirements)
		}
		return fmt.Errorf("no instance type satisfied request")
	}
	n.Pods = append(n.Pods, pod)
	n.InstanceTypeOptions = instanceTypes
	n.requests = requests
	n.Constraints.Requirements = requirements
	return nil
}

func (n *Node) String() string {
	var itSb strings.Builder
	for i, it := range n.InstanceTypeOptions {
		// print the first 5 instance types only (indices 0-4)
		if i > 4 {
			fmt.Fprintf(&itSb, " and %d other(s)", len(n.InstanceTypeOptions)-i)
			break
		} else if i > 0 {
			fmt.Fprint(&itSb, ", ")
		}
		fmt.Fprint(&itSb, it.Name())
	}
	return fmt.Sprintf("node with %d pods requesting %s from types %s", len(n.Pods), resources.String(n.requests), itSb.String())
}

func (n Node) ToConstraints() *v1alpha5.Constraints {
	return &v1alpha5.Constraints{
		Labels:               n.Constraints.Labels,
		Taints:               n.Constraints.Taints,
		Requirements:         n.Constraints.Requirements.ToNodeSelector(),
		KubeletConfiguration: n.Constraints.KubeletConfiguration,
		Provider:             n.Constraints.Provider,
	}
}
