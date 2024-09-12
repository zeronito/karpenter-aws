package memoryoverhead

import (
	"context"
	"fmt"
	v1 "github.com/aws/karpenter-provider-aws/pkg/apis/v1"
	"github.com/aws/karpenter-provider-aws/pkg/providers/instancetype"
	"time"

	"github.com/awslabs/operatorpkg/singleton"
	"github.com/samber/lo"
	corev1 "k8s.io/api/core/v1"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/karpenter/pkg/operator/injection"

	karpv1 "sigs.k8s.io/karpenter/pkg/apis/v1"
)

const (
	fastRequeueThreshold = 20
	fastRequeueDuration  = 20 * time.Second
	slowRequeueDuration  = 10 * time.Minute
)

type Controller struct {
	kubeClient      client.Client
	successfulCount uint64 // keeps track of successful reconciles for more aggressive requeueing near the start of the controller
}

func NewController(kubeClient client.Client) *Controller {
	return &Controller{
		kubeClient:      kubeClient,
		successfulCount: 0,
	}
}

func (c *Controller) Reconcile(ctx context.Context) (reconcile.Result, error) {
	ctx = injection.WithControllerName(ctx, "instancetypes.memoryoverhead")

	nodeClassList := &v1.EC2NodeClassList{}
	if err := c.kubeClient.List(ctx, nodeClassList); err != nil {
		return reconcile.Result{}, err
	}

	nodeClaimList := &karpv1.NodeClaimList{}
	if err := c.kubeClient.List(ctx, nodeClaimList); err != nil {
		return reconcile.Result{}, err
	}

	nodeList := &corev1.NodeList{}
	if err := c.kubeClient.List(ctx, nodeList); err != nil {
		return reconcile.Result{}, err
	}

	nodeMap := lo.Associate(nodeList.Items, func(node corev1.Node) (string, *corev1.Node) {
		return node.Name, &node
	})

	for _, nodeClass := range nodeClassList.Items {

		// Iterate over NodeClaims to find matching instance types
		for _, nodeClaim := range nodeClaimList.Items {
			// Skip NodeClaims that don't match current NodeClass
			if nodeClaim.Spec.NodeClassRef.Name != nodeClass.Name {
				continue
			}

			// Skip non-registered nodes
			// Is this check necessary?
			node, exists := nodeMap[nodeClaim.Status.NodeName]
			if !exists {
				continue
			}

			// Build cache key from combination of NodeClass hash and instance type
			instType := nodeClaim.Labels[corev1.LabelInstanceTypeStable]
			key := fmt.Sprintf("%d-%s", nodeClass.Hash(), instType)

			// Add memory capacity in the map
			actual := node.Status.Capacity.Memory().Value() / 1024 / 1024

			instancetype.MemoryOverheadMebibytes.SetDefault(key, actual)
		}
	}
	c.successfulCount++
	return reconcile.Result{RequeueAfter: lo.Ternary(c.successfulCount <= fastRequeueThreshold, fastRequeueDuration, slowRequeueDuration)}, nil
}

func (c *Controller) Register(_ context.Context, m manager.Manager) error {
	return controllerruntime.NewControllerManagedBy(m).
		Named("instancetypes.memoryoverhead").
		WatchesRawSource(singleton.Source()).
		Complete(singleton.AsReconciler(c))
}
