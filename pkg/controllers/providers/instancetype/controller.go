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

package instancetype

import (
	"context"
	"fmt"
	"time"

	lop "github.com/samber/lo/parallel"
	"go.uber.org/multierr"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"sigs.k8s.io/karpenter/pkg/operator/controller"

	"github.com/aws/karpenter-provider-aws/pkg/providers/instancetype"
)

type Controller struct {
	instancetypeProvider instancetype.Provider
}

func NewController(instancetypeProvider instancetype.Provider) *Controller {
	return &Controller{
		instancetypeProvider: instancetypeProvider,
	}
}

func (c *Controller) Reconcile(ctx context.Context, _ reconcile.Request) (reconcile.Result, error) {
	work := []func(ctx context.Context) error{
		c.instancetypeProvider.UpdateInstanceTypes,
		c.instancetypeProvider.UpdateInstanceTypeOfferings,
	}
	errs := make([]error, len(work))
	lop.ForEach(work, func(f func(ctx context.Context) error, i int) {
		if err := f(ctx); err != nil {
			errs[i] = err
		}
	})
	if err := multierr.Combine(errs...); err != nil {
		return reconcile.Result{}, fmt.Errorf("updating instancetype, %w", err)
	}
	return reconcile.Result{RequeueAfter: 12 * time.Hour}, nil
}

func (c *Controller) Name() string {
	return "providers.instancetype"
}

func (c *Controller) Builder(_ context.Context, m manager.Manager) controller.Builder {
	// Includes a default exponential failure rate limiter of base: time.Millisecond, and max: 1000*time.Second
	return controller.NewSingletonManagedBy(m)
}
