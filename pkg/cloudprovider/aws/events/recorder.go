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

package events

import (
	v1 "k8s.io/api/core/v1"
	"k8s.io/client-go/tools/record"

	"github.com/aws/karpenter/pkg/events"
)

type recorder struct {
	rec record.EventRecorder
	events.Recorder
}

type Recorder interface {
	events.Recorder

	// EC2SpotInterruptionWarning is called when EC2 sends a spot interruption 2-minute warning for the node from the SQS queue
	EC2SpotInterruptionWarning(*v1.Node)
	// EC2SpotRebalanceRecommendation is called when EC2 sends a rebalance recommendation for the node from the SQS queue
	EC2SpotRebalanceRecommendation(*v1.Node)
	// EC2HealthWarning is called when EC2 sends a health warning notification for a health issue for the node from the SQS queue
	EC2HealthWarning(*v1.Node)
	// EC2StateTerminating is called when EC2 sends a state change notification for a node that is changing to a terminating/shutting-down state
	EC2StateTerminating(*v1.Node)
	// EC2StateStopping is called when EC2 sends a state change notification for a node that is changing to a stopping/stopped state
	EC2StateStopping(*v1.Node)
	// TerminatingNodeOnNotification is called when a notification that is sent to the notification controller triggers node deletion
	TerminatingNodeOnNotification(*v1.Node)
}

func NewRecorder(r events.Recorder) Recorder {
	return recorder{
		rec:      r.EventRecorder(),
		Recorder: r,
	}
}

func (r recorder) EventRecorder() record.EventRecorder {
	return r.rec
}

func (r recorder) EC2SpotInterruptionWarning(node *v1.Node) {
	r.rec.Eventf(node, "Normal", "EC2SpotInterruptionWarning", "Node %s event: EC2 triggered a spot interruption warning for the node", node.Name)
}

func (r recorder) EC2SpotRebalanceRecommendation(node *v1.Node) {
	r.rec.Eventf(node, "Normal", "EC2RebalanceRecommendation", "Node %s event: EC2 triggered a spot rebalance recommendation for the node", node.Name)
}

func (r recorder) EC2HealthWarning(node *v1.Node) {
	r.rec.Eventf(node, "Normal", "EC2HealthWarning", "Node %s event: EC2 triggered a health warning for the node", node.Name)
}

func (r recorder) EC2StateTerminating(node *v1.Node) {
	r.rec.Eventf(node, "Normal", "EC2StateTerminating", `Node %s event: EC2 node is terminating"`, node.Name)
}

func (r recorder) EC2StateStopping(node *v1.Node) {
	r.rec.Eventf(node, "Normal", "EC2StateStopping", `Node %s event: EC2 node is stopping"`, node.Name)
}

func (r recorder) TerminatingNodeOnNotification(node *v1.Node) {
	r.rec.Eventf(node, "Normal", "AWSNotificationTerminateNode", "Node %s event: Notification triggered termination for the node", node.Name)
}
