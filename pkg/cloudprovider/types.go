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

package cloudprovider

import (
	"context"

	"github.com/awslabs/karpenter/pkg/apis/autoscaling/v1alpha1"
	v1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// Factory instantiates the cloud provider's resources
type Factory interface {
	// NodeGroupFor returns a node group for the provided spec
	NodeGroupFor(sng *v1alpha1.ScalableNodeGroupSpec) NodeGroup
	// QueueFor returns a queue for the provided spec
	QueueFor(queue *v1alpha1.QueueSpec) Queue

	// Capacity returns a provisioner for the provider to create instances
	Capacity() Capacity
}

// Queue abstracts all provider specific behavior for Queues
type Queue interface {
	// Name returns the name of the queue
	Name() string
	// Length returns the length of the queue
	Length() (int64, error)
	// OldestMessageAge returns the age of the oldest message
	OldestMessageAgeSeconds() (int64, error)
}

// NodeGroup abstracts all provider specific behavior for NodeGroups.
// It is meant to be used by controllers.
type NodeGroup interface {
	// SetReplicas sets the desired replicas for the node group
	SetReplicas(count int32) error
	// GetReplicas returns the number of schedulable replicas in the node group
	GetReplicas() (int32, error)
	// Stabilized returns true if a node group is not currently adding or
	// removing replicas. Otherwise, returns false with a message.
	Stabilized() (bool, string, error)
}

// Capacity provisions a set of nodes that fulfill a set of constraints.
type Capacity interface {
	// Create a set of nodes to fulfill the desired capacity given constraints.
	Create(context.Context, CapacityConstraints) error
}

// CapacityConstraints lets the controller define the desired capacity,
// avalability zone, architecture for the desired nodes.
type CapacityConstraints struct {
	// Zone constrains where a node can be created within a region.
	Zone string
	// Resources constrains the minimum capacity to provision (e.g. CPU, Memory).
	Resources v1.ResourceList
	// Overhead resources per node from system resources such a kubelet and daemonsets.
	Overhead v1.ResourceList
	// Architecture constrains the underlying hardware architecture.
	Architecture *Architecture
}

// Architecture constrains the underlying node's compilation architecture.
type Architecture string

const (
	Linux386 Architecture = "linux/386"
	// LinuxAMD64 Architecture = "linux/amd64" TODO
)

// Options are injected into cloud providers' factories
type Options struct {
	Client client.Client
}
