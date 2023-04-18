/*
Copyright 2020 The Crossplane Authors.

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

package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"time"

	"gopkg.in/alecthomas/kingpin.v2"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/feature"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/resource"

	"github.com/crossplane/provider-ceph/apis"
	"github.com/crossplane/provider-ceph/apis/v1alpha1"
	ceph "github.com/crossplane/provider-ceph/internal/controller"
	"github.com/crossplane/provider-ceph/internal/controller/features"
)

var defaultZapConfig = map[string]string{
	"zap-encoder":          "json",
	"zap-stacktrace-level": "error",
	"zap-time-encoding":    "rfc3339nano",
}

func main() {
	var (
		app            = kingpin.New(filepath.Base(os.Args[0]), "Ceph support for Crossplane.").DefaultEnvars()
		debug          = app.Flag("debug", "Run with debug logging.").Short('d').Bool()
		leaderElection = app.Flag("leader-election", "Use leader election for the controller manager.").Short('l').Default("false").OverrideDefaultFromEnvar("LEADER_ELECTION").Bool()
		leaderRenew    = app.Flag("leader-renew", "Set leader election renewal.").Short('r').Default("10s").OverrideDefaultFromEnvar("LEADER_ELECTION_RENEW").Duration()

		syncInterval     = app.Flag("sync", "How often all resources will be double-checked for drift from the desired state.").Short('s').Default("1h").Duration()
		pollInterval     = app.Flag("poll", "How often individual resources will be checked for drift from the desired state").Short('p').Default("1m").Duration()
		maxReconcileRate = app.Flag("max-reconcile-rate", "The global maximum rate per second at which resources may checked for drift from the desired state.").Default("10").Int()

		namespace                  = app.Flag("namespace", "Namespace used to set as default scope in default secret store config.").Default("crossplane-system").Envar("POD_NAMESPACE").String()
		enableExternalSecretStores = app.Flag("enable-external-secret-stores", "Enable support for ExternalSecretStores.").Default("false").Envar("ENABLE_EXTERNAL_SECRET_STORES").Bool()
	)

	var opts zap.Options
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	flag.CommandLine.VisitAll(func(f *flag.Flag) {
		defaultValue, ok := defaultZapConfig[f.Name]
		if !ok {
			defaultValue = f.DefValue
		}

		app.Flag(f.Name, f.Usage).Default(defaultValue).String()
	})

	kingpin.MustParse(app.Parse(os.Args[1:]))

	zl := zap.New(zap.UseDevMode(*debug), zap.UseFlagOptions(&opts))
	ctrl.SetLogger(zl)
	klog.SetLogger(zl)

	log := logging.NewLogrLogger(zl.WithName("provider-ceph"))

	cfg, err := ctrl.GetConfig()
	kingpin.FatalIfError(err, "Cannot get API server rest config")

	renewDeadline := time.Duration(*leaderRenew) * time.Second
	leaseDuration := time.Duration(int(1.2*float64(*leaderRenew))) * time.Second
	leaderRetryDuration := renewDeadline / 2

	mgr, err := ctrl.NewManager(ratelimiter.LimitRESTConfig(cfg, *maxReconcileRate), ctrl.Options{
		SyncPeriod: syncInterval,

		LeaderElection:             *leaderElection,
		LeaderElectionID:           "crossplane-leader-election-provider-ceph-ibyaiby",
		LeaderElectionNamespace:    *namespace,
		LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
		RenewDeadline:              &renewDeadline,
		LeaseDuration:              &leaseDuration,
		RetryPeriod:                &leaderRetryDuration,
	})
	kingpin.FatalIfError(err, "Cannot create controller manager")
	kingpin.FatalIfError(apis.AddToScheme(mgr.GetScheme()), "Cannot add Ceph APIs to scheme")

	o := controller.Options{
		Logger:                  log,
		MaxConcurrentReconciles: *maxReconcileRate,
		PollInterval:            *pollInterval,
		GlobalRateLimiter:       ratelimiter.NewGlobal(*maxReconcileRate),
		Features:                &feature.Flags{},
	}

	if *enableExternalSecretStores {
		o.Features.Enable(features.EnableAlphaExternalSecretStores)
		log.Info("Alpha feature enabled", "flag", features.EnableAlphaExternalSecretStores)

		// Ensure default store config exists.
		kingpin.FatalIfError(resource.Ignore(kerrors.IsAlreadyExists, mgr.GetClient().Create(context.Background(), &v1alpha1.StoreConfig{
			ObjectMeta: metav1.ObjectMeta{
				Name: "default",
			},
			Spec: v1alpha1.StoreConfigSpec{
				// NOTE(turkenh): We only set required spec and expect optional
				// ones to properly be initialized with CRD level default values.
				SecretStoreConfig: xpv1.SecretStoreConfig{
					DefaultScope: *namespace,
				},
			},
		})), "cannot create default store config")
	}

	kingpin.FatalIfError(ceph.Setup(mgr, o), "Cannot setup Ceph controllers")
	kingpin.FatalIfError(mgr.Start(ctrl.SetupSignalHandler()), "Cannot start controller manager")
}
