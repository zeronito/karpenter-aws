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

package aws

import (
	"context"
	"fmt"
<<<<<<< HEAD
=======
	"strings"
>>>>>>> 8d6d79b (implement capacity-type in the cloud provider)

	"github.com/awslabs/karpenter/pkg/apis/provisioning/v1alpha1"
	"github.com/awslabs/karpenter/pkg/cloudprovider"
	"github.com/awslabs/karpenter/pkg/packing"
	"github.com/awslabs/karpenter/pkg/utils/functional"
	"go.uber.org/zap"
	v1 "k8s.io/api/core/v1"
)

const (
	nodeLabelPrefix      = "node.k8s.aws"
	capacityTypeSpot     = "spot"
	capacityTypeOnDemand = "on-demand"
	maxInstanceTypes     = 10
)

var (
	capacityTypeLabel = fmt.Sprintf("%s/capacity-type", nodeLabelPrefix)
)

// Capacity cloud provider implementation using AWS Fleet.
type Capacity struct {
	spec                   *v1alpha1.ProvisionerSpec
	nodeFactory            *NodeFactory
	packer                 packing.Packer
	instanceProvider       *InstanceProvider
	vpcProvider            *VPCProvider
	launchTemplateProvider *LaunchTemplateProvider
	instanceTypeProvider   *InstanceTypeProvider
}

// AWSConstraints are AWS specific constraints
type AWSConstraints struct {
	cloudprovider.Constraints
	CapacityType string
}

func NewAWSConstraints(constraints *cloudprovider.Constraints) *AWSConstraints {
	capacityType, ok := constraints.Labels[capacityTypeLabel]
	if !ok {
		capacityType = capacityTypeOnDemand
	}
	return &AWSConstraints{
		Constraints:  *constraints,
		CapacityType: capacityType,
	}
}

// Create a set of nodes given the constraints.
func (c *Capacity) Create(ctx context.Context, constraints *cloudprovider.Constraints) ([]cloudprovider.Packing, error) {
	// 1. Create AWS Cloud Provider constraints and apply defaults
	awsConstraints := NewAWSConstraints(constraints)

	// 2. Retrieve normalized zones from constraints or all zones that the cluster spans if no zonal constraints are specified
	zonalSubnetOptions, err := c.vpcProvider.GetZonalSubnets(ctx, awsConstraints, c.spec.Cluster.Name)
	if err != nil {
		return nil, fmt.Errorf("getting zonal subnets, %w", err)
	}

	// 3. Filter for instance types that fit constraints
	zonalInstanceTypes, err := c.instanceTypeProvider.Get(ctx, zonalSubnetOptions, awsConstraints)
	if err != nil {
		return nil, fmt.Errorf("filtering instance types by constraints, %w", err)
	}

	// 4. Compute Packing given the pods and instance types
	instancePackings := c.packer.Pack(ctx, awsConstraints.Pods, zonalInstanceTypes, constraints)

	zap.S().Debugf("Computed packings for %d pod(s) onto %d node(s)", len(awsConstraints.Pods), len(instancePackings))
	launchTemplate, err := c.launchTemplateProvider.Get(ctx, c.spec.Cluster, awsConstraints)
	if err != nil {
		return nil, fmt.Errorf("getting launch template, %w", err)
	}

	// 5. Create Instances
	var instanceIDs []*string
	podsForInstance := make(map[string][]*v1.Pod)
	for _, packing := range instancePackings {
		if len(packing.InstanceTypes) > maxInstanceTypes {
			packing.InstanceTypes = packing.InstanceTypes[:maxInstanceTypes]
		}
		instanceID, err := c.instanceProvider.Create(ctx, launchTemplate, packing.InstanceTypes, zonalSubnetOptions, awsConstraints.CapacityType)
		if err != nil {
			// TODO Aggregate errors and continue
			return nil, fmt.Errorf("creating capacity %w", err)
		}
		podsForInstance[*instanceID] = packing.Pods
		instanceIDs = append(instanceIDs, instanceID)
	}

	// 6. Convert to Nodes
	nodes, err := c.nodeFactory.For(ctx, instanceIDs)
	if err != nil {
		return nil, fmt.Errorf("determining nodes, %w", err)
	}
	nodePackings := []cloudprovider.Packing{}
	for instanceID, node := range nodes {
		node.Labels = awsConstraints.Labels
		node.Spec.Taints = awsConstraints.Taints
		nodePackings = append(nodePackings, cloudprovider.Packing{
			Node: node,
			Pods: podsForInstance[instanceID],
		})
	}
	return nodePackings, nil
}

func (c *Capacity) Delete(ctx context.Context, nodes []*v1.Node) error {
	return c.instanceProvider.Terminate(ctx, nodes)
}

func (c *Capacity) GetInstanceTypes(ctx context.Context) ([]string, error) {
	return c.instanceTypeProvider.GetAllInstanceTypeNames(ctx)
}

func (c *Capacity) GetZones(ctx context.Context) ([]string, error) {
	azs, err := c.vpcProvider.GetAllZones(ctx)
	if err != nil {
		return nil, err
	}
	zones := []string{}
	for _, az := range azs {
		zones = append(zones, *az.ZoneName, *az.ZoneId)
	}
	return zones, nil
}

func (c *Capacity) GetArchitectures(ctx context.Context) ([]string, error) {
	return []string{
		v1alpha1.ArchitectureAmd64,
		v1alpha1.ArchitectureArm64,
	}, nil
}

func (c *Capacity) GetOperatingSystems(ctx context.Context) ([]string, error) {
	return []string{
		v1alpha1.OperatingSystemLinux,
	}, nil
}

func (c *Capacity) ValidateLabels(labels map[string]string) error {
	for key, value := range labels {
		if strings.HasPrefix(key, nodeLabelPrefix) {
			switch key {
			case capacityTypeLabel:
				capacityTypes := []string{capacityTypeSpot, capacityTypeOnDemand}
				if !functional.ContainsString(capacityTypes, value) {
					return fmt.Errorf("%s must be one of %v", key, capacityTypes)
				}
			default:
				return fmt.Errorf("%s/* is reserved for AWS cloud provider use", nodeLabelPrefix)
			}
		}
	}
	return nil
}
