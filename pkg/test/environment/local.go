package environment

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"time"

	"github.com/ellistarn/karpenter/pkg/controllers"
	"github.com/ellistarn/karpenter/pkg/utils/log"
	"github.com/ellistarn/karpenter/pkg/utils/project"
	"github.com/onsi/gomega/ghttp"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/util/wait"
	controllerruntime "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/envtest"
	controllerruntimezap "sigs.k8s.io/controller-runtime/pkg/log/zap"
)

/*
Local is an Environment for e2e local testing. It stands up an API Server, ETCD,
and a controller-runtime manager. It's possible to run multiple environments
simultaneously, as the ports are randomized. A common use case for this is
parallel tests using ginkgo's parallelization functionality. The environment is
typically instantiated once in a test file and re-used between different test
cases. Resources for each test should be isolated into its own namespace.

env := new Local(func(local *Local) {
	// Register test controller with manager
	controllerruntime.NewControllerManagedBy(local.Manager).For(...)
	return nil
})
BeforeSuite(func() { env.Start() })
AfterSuite(func() { env.Stop() })

*/
type Local struct {
	envtest.Environment
	Manager controllers.Manager
	Server  *ghttp.Server

	options []LocalOption
	stopCh  chan struct{}
}

// LocalOption passes the Local environment to an option function. This is
// useful for registering controllers with the controller-runtime manager or for
// customizing Client, Scheme, or other variables.
type LocalOption func(env *Local)

func NewLocal(options ...LocalOption) Environment {
	log.Setup(controllerruntimezap.UseDevMode(false))
	return &Local{
		Environment: envtest.Environment{
			CRDDirectoryPaths: []string{project.RelativeToRoot("config/crd/bases")},
			WebhookInstallOptions: envtest.WebhookInstallOptions{
				DirectoryPaths: []string{project.RelativeToRoot("config/webhook")},
			},
		},
		Server:  ghttp.NewServer(),
		stopCh:  make(chan struct{}),
		options: options,
	}
}

func (e *Local) NewNamespace() (*Namespace, error) {
	client, err := client.New(e.Manager.GetConfig(), client.Options{
		Scheme: e.Manager.GetScheme(),
		Mapper: e.Manager.GetRESTMapper(),
	})
	if err != nil {
		return nil, err
	}
	ns := NewNamespace(client)
	if err := e.Manager.GetClient().Create(context.Background(), &ns.Namespace); err != nil {
		return nil, err
	}

	go func() {
		<-e.stopCh
		if err := e.Manager.GetClient().Delete(context.Background(), &ns.Namespace); err != nil {
			zap.S().Errorf("Failed to tear down namespace, %w", err)
		}
	}()
	return ns, nil
}

func (e *Local) Start() (err error) {
	// Environment
	if _, err := e.Environment.Start(); err != nil {
		return fmt.Errorf("starting environment, %w", err)
	}

	// Manager
	e.Manager = controllers.NewManagerOrDie(e.Config, controllerruntime.Options{
		CertDir:            e.WebhookInstallOptions.LocalServingCertDir,
		Host:               e.WebhookInstallOptions.LocalServingHost,
		Port:               e.WebhookInstallOptions.LocalServingPort,
		MetricsBindAddress: "0", // Skip the metrics server to avoid port conflicts for parallel testing
	})

	// options
	for _, option := range e.options {
		option(e)
	}

	// Close on interrupt
	go func() {
		<-controllerruntime.SetupSignalHandler()
		close(e.stopCh)
	}()

	// Start manager
	go func() {
		if err := e.Manager.Start(e.stopCh); err != nil {
			zap.S().Fatal(err)
		}
	}()

	// Wait for the manager to start
	backOffOpts := wait.Backoff{
		Duration: 10 * time.Millisecond,
		Factor:   1.5,
		Steps:    10,
		Cap:      1 * time.Second,
	}
	return wait.ExponentialBackoff(backOffOpts, func() (bool, error) {
		url := fmt.Sprintf("%s:%d", e.WebhookInstallOptions.LocalServingHost, e.WebhookInstallOptions.LocalServingPort)
		dialer := tls.Dialer{NetDialer: &net.Dialer{}, Config: &tls.Config{InsecureSkipVerify: true}}
		ctx, cancel := context.WithDeadline(context.Background(), time.Now().Add(backOffOpts.Cap))
		defer cancel()
		conn, err := dialer.DialContext(ctx, "tcp", url)
		if err != nil || conn.Close() != nil {
			return false, nil // keeps trying
		}
		return true, nil // stops trying
	})
}

func (e *Local) Stop() error {
	close(e.stopCh)
	if err := e.Environment.Stop(); err != nil {
		return err
	}
	return nil
}
