// Package main is the entrypoint for provider-valkey.
package main

import (
	"os"
	"path/filepath"

	"github.com/alecthomas/kingpin/v2"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	"github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/feature"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/crossplane-runtime/v2/pkg/ratelimiter"

	"github.com/lukasMeko/provider-valkey/apis/v1alpha1"
	pkgcontroller "github.com/lukasMeko/provider-valkey/internal/controller"
)

func main() {
	var (
		app              = kingpin.New(filepath.Base(os.Args[0]), "Crossplane Valkey Provider").DefaultEnvars()
		debug            = app.Flag("debug", "Run with debug logging.").Short('d').Bool()
		syncInterval     = app.Flag("sync", "Controller manager sync period duration.").Short('s').Default("1h").Duration()
		pollInterval     = app.Flag("poll", "Poll interval for reconciliation.").Default("1m").Duration()
		maxReconcileRate = app.Flag("max-reconcile-rate", "Maximum number of concurrent reconciliation operations.").Default("10").Int()
	)
	kingpin.MustParse(app.Parse(os.Args[1:]))

	ctrl.SetLogger(zap.New(zap.UseDevMode(*debug)))
	log := logging.NewLogrLogger(ctrl.Log.WithName("provider-valkey"))

	scheme := runtime.NewScheme()
	if err := clientgoscheme.AddToScheme(scheme); err != nil {
		log.Info("cannot add client-go scheme", "error", err)
		os.Exit(1)
	}
	if err := v1alpha1.SchemeBuilder.AddToScheme(scheme); err != nil {
		log.Info("cannot add provider scheme", "error", err)
		os.Exit(1)
	}

	mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
		Scheme: scheme,
		Cache: cache.Options{
			SyncPeriod: syncInterval,
		},
	})
	if err != nil {
		log.Info("cannot create controller manager", "error", err)
		os.Exit(1)
	}

	o := controller.Options{
		Logger:                  log,
		MaxConcurrentReconciles: *maxReconcileRate,
		PollInterval:            *pollInterval,
		GlobalRateLimiter:       ratelimiter.NewGlobal(*maxReconcileRate),
		Features:                &feature.Flags{},
	}

	if err := pkgcontroller.Setup(mgr, o); err != nil {
		log.Info("cannot setup controllers", "error", err)
		os.Exit(1)
	}

	log.Info("starting provider")
	if err := mgr.Start(ctrl.SetupSignalHandler()); err != nil {
		log.Info("cannot start controller manager", "error", err)
		os.Exit(1)
	}
}
