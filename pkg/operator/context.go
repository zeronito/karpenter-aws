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

package operator

import (
	"context"
	"runtime/debug"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"k8s.io/client-go/kubernetes"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/util/flowcontrol"
	"k8s.io/utils/clock"
	"knative.dev/pkg/configmap/informer"
	knativeinjection "knative.dev/pkg/injection"
	"knative.dev/pkg/logging"
	"knative.dev/pkg/system"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/manager"

	"github.com/aws/karpenter/pkg/apis"
	"github.com/aws/karpenter/pkg/config"
	"github.com/aws/karpenter/pkg/events"
	"github.com/aws/karpenter/pkg/utils/injection"
	"github.com/aws/karpenter/pkg/utils/project"
)

const (
	appName   = "karpenter"
	component = "controller"
)

var (
	scheme = runtime.NewScheme()
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(apis.AddToScheme(scheme))
}

// Context is the root context for the operator and exposes shared components used by the entire process
type Context struct {
	context.Context
	Recorder   events.Recorder
	Config     config.Config
	KubeClient client.Client
	Clientset  *kubernetes.Clientset
	Clock      clock.Clock
	Options    *Options
	StartAsync <-chan struct{}
}

func NewOrDie() (Context, manager.Manager) {
	options := NewOptions().MustParse()

	// Setup Client
	restConfig := controllerruntime.GetConfigOrDie()
	restConfig.RateLimiter = flowcontrol.NewTokenBucketRateLimiter(float32(options.KubeClientQPS), options.KubeClientBurst)
	restConfig.UserAgent = appName
	clientSet := kubernetes.NewForConfigOrDie(restConfig)

	// Set up logger and watch for changes to log level defined in the ConfigMap `config-logging`
	cmw := informer.NewInformedWatcher(clientSet, system.Namespace())
	ctx := injection.LoggingContextOrDie(component, restConfig, cmw)
	ctx = knativeinjection.WithConfig(ctx, restConfig)
	ctx = WithOptions(ctx, *options)

	logging.FromContext(ctx).Infof("Initializing with version %s", project.Version)
	if options.MemoryLimit > 0 {
		newLimit := int64(float64(options.MemoryLimit) * 0.9)
		logging.FromContext(ctx).Infof("Setting GC memory limit to %d, container limit = %d", newLimit, options.MemoryLimit)
		debug.SetMemoryLimit(newLimit)
	}

	cfg, err := config.New(ctx, clientSet, cmw)
	if err != nil {
		// this does not happen if the config map is missing or invalid, only if some other error occurs
		logging.FromContext(ctx).Fatalf("unable to load config, %s", err)
	}
	if err := cmw.Start(ctx.Done()); err != nil {
		logging.FromContext(ctx).Errorf("watching configmaps, config changes won't be applied immediately, %s", err)
	}

	manager := NewManagerOrDie(ctx, restConfig, options)
	recorder := events.NewRecorder(manager.GetEventRecorderFor(appName))
	recorder = events.NewLoadSheddingRecorder(recorder)
	recorder = events.NewDedupeRecorder(recorder)

	return Context{
		Context:    ctx,
		Recorder:   recorder,
		Config:     cfg,
		Clientset:  clientSet,
		KubeClient: manager.GetClient(),
		Clock:      clock.RealClock{},
		Options:    options,
		StartAsync: manager.Elected(),
	}, manager
}
