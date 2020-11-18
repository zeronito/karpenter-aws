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

package v1alpha1

import (
	"testing"

	v1alpha1 "github.com/awslabs/karpenter/pkg/apis/autoscaling/v1alpha1"
	"github.com/awslabs/karpenter/pkg/cloudprovider"
	"github.com/awslabs/karpenter/pkg/cloudprovider/fake"
	"knative.dev/pkg/ptr"

	"github.com/awslabs/karpenter/pkg/test/environment"
	. "github.com/awslabs/karpenter/pkg/test/expectations"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"
	"sigs.k8s.io/controller-runtime/pkg/envtest/printer"
)

func TestAPIs(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecsWithDefaultAndCustomReporters(t,
		"ScalableNodeGroup",
		[]Reporter{printer.NewlineReporter{}})
}

var fakeCloudProvider = fake.NewFactory(cloudprovider.Options{})
var fakeController = &Controller{CloudProvider: fakeCloudProvider}
var env environment.Environment = environment.NewLocal(func(e *environment.Local) {
	e.Manager.Register(fakeController)
})

var _ = BeforeSuite(func() {
	Expect(env.Start()).To(Succeed(), "Failed to start environment")
})

var _ = AfterSuite(func() {
	Expect(env.Stop()).To(Succeed(), "Failed to stop environment")
})

var _ = Describe("Examples", func() {
	var ns *environment.Namespace
	var sng *v1alpha1.ScalableNodeGroup
	var ng cloudprovider.NodeGroup
	var desiredReplicas = ptr.Int32(5)
	var dummyMessage = "test message"

	BeforeEach(func() {
		var err error
		ns, err = env.NewNamespace()
		Expect(err).NotTo(HaveOccurred())
		sng = &v1alpha1.ScalableNodeGroup{}
		sng.Spec.Replicas = desiredReplicas
		ng = fakeController.CloudProvider.NodeGroupFor(&sng.Spec)

	})

	Context("ScalableNodeGroup", func() {
		It("should be created", func() {
			Expect(ns.ParseResources("docs/examples/reserved-capacity-utilization.yaml", sng)).To(Succeed())
			ExpectCreated(ns.Client, sng)
			ExpectEventuallyHappy(ns.Client, sng)
			ExpectDeleted(ns.Client, sng)
		})
	})

	Context("Basic ScalableNodeGroup Reconcile tests", func() {
		It("Test reconciler to scale up nodes", func() {
			Expect(fakeController.Reconcile(sng)).To(Succeed())
			Expect(ng.GetReplicas()).To(Equal(*desiredReplicas))
		})

		It("Test reconciler to scale down nodes", func() {
			ng.SetReplicas(10) // set existing replicas higher than desired
			Expect(fakeController.Reconcile(sng)).To(Succeed())
			Expect(ng.GetReplicas()).To(Equal(*desiredReplicas))
		})

		It("Test reconciler to make no change to node count", func() {
			ng.SetReplicas(*desiredReplicas) // set existing replicas equal to desired
			Expect(fakeController.Reconcile(sng)).To(Succeed())
			Expect(ng.GetReplicas()).To(Equal(*desiredReplicas))
		})
	})

})
