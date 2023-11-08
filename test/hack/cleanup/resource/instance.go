package resource

import (
	"context"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/ec2"
	ec2types "github.com/aws/aws-sdk-go-v2/service/ec2/types"
	"github.com/samber/lo"
)

type Instance struct {
	ec2Client *ec2.Client
}

func NewInstance(ec2Client *ec2.Client) *Instance {
	return &Instance{ec2Client: ec2Client}
}

func (i *Instance) Type() string {
	return "Instances"
}

func (i *Instance) GetExpired(ctx context.Context, expirationTime time.Time) (ids []string, err error) {
	var nextToken *string
	for {
		out, err := i.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			Filters: []ec2types.Filter{
				{
					Name:   lo.ToPtr("Instance-state-name"),
					Values: []string{string(ec2types.InstanceStateNameRunning)},
				},
				{
					Name:   lo.ToPtr("tag-key"),
					Values: []string{karpenterProvisionerNameTag, karpenterNodePoolTag},
				},
			},
			NextToken: nextToken,
		})
		if err != nil {
			return ids, err
		}

		for _, res := range out.Reservations {
			for _, instance := range res.Instances {
				if lo.FromPtr(instance.LaunchTime).Before(expirationTime) {
					ids = append(ids, lo.FromPtr(instance.InstanceId))
				}
			}
		}

		nextToken = out.NextToken
		if nextToken == nil {
			break
		}
	}
	return ids, err
}

func (i *Instance) Get(ctx context.Context, clusterName string) (ids []string, err error) {
	var nextToken *string

	for {
		out, err := i.ec2Client.DescribeInstances(ctx, &ec2.DescribeInstancesInput{
			Filters: []ec2types.Filter{
				{
					Name:   lo.ToPtr("Instance-state-name"),
					Values: []string{string(ec2types.InstanceStateNameRunning)},
				},
				{
					Name:   lo.ToPtr("tag:" + karpenterClusterNameTag),
					Values: []string{clusterName},
				},
			},
			NextToken: nextToken,
		})
		if err != nil {
			return ids, err
		}

		for _, res := range out.Reservations {
			for _, instance := range res.Instances {
				ids = append(ids, lo.FromPtr(instance.InstanceId))
			}
		}

		nextToken = out.NextToken
		if nextToken == nil {
			break
		}
	}
	return ids, err
}

func (i *Instance) Cleanup(ctx context.Context, ids []string) ([]string, error) {
	if _, err := i.ec2Client.TerminateInstances(ctx, &ec2.TerminateInstancesInput{
		InstanceIds: ids,
	}); err != nil {
		return nil, err
	}
	return ids, nil
}
