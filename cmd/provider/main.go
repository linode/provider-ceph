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

//go:generate go get github.com/maxbrunsfeld/counterfeiter/v6

import (
	"context"
	"errors"
	"flag"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/alecthomas/kingpin/v2"
	"github.com/go-logr/logr"
	"github.com/linode/provider-ceph/internal/otel/traces"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.uber.org/zap/zapcore"
	kerrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	xpv1 "github.com/crossplane/crossplane-runtime/apis/common/v1"
	"github.com/crossplane/crossplane-runtime/pkg/controller"
	"github.com/crossplane/crossplane-runtime/pkg/feature"
	"github.com/crossplane/crossplane-runtime/pkg/logging"
	"github.com/crossplane/crossplane-runtime/pkg/meta"
	"github.com/crossplane/crossplane-runtime/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/pkg/resource"
	"github.com/crossplane/crossplane-runtime/pkg/statemetrics"

	"github.com/linode/provider-ceph/apis"
	providercephv1alpha1 "github.com/linode/provider-ceph/apis/provider-ceph/v1alpha1"
	"github.com/linode/provider-ceph/apis/v1alpha1"
	"github.com/linode/provider-ceph/internal/backendstore"
	"github.com/linode/provider-ceph/internal/controller/bucket"
	"github.com/linode/provider-ceph/internal/controller/providerconfig"
	"github.com/linode/provider-ceph/internal/controller/providerconfig/backendmonitor"
	"github.com/linode/provider-ceph/internal/controller/providerconfig/healthcheck"
	"github.com/linode/provider-ceph/internal/controller/s3clienthandler"

	"github.com/linode/provider-ceph/internal/features"
)

var defaultZapConfig = map[string]string{
	"zap-encoder":          "json",
	"zap-stacktrace-level": "error",
	"zap-time-encoding":    "rfc3339nano",
}

var (
	errFailedToSyncCache = errors.New("failed to sync kube client cache reader")
)

//nolint:maintidx // Function requires a lot of setup operations.
func main() {
	var (
		app            = kingpin.New(filepath.Base(os.Args[0]), "Ceph support for Crossplane.").DefaultEnvars()
		leaderElection = app.Flag("leader-election", "Use leader election for the controller manager.").Short('l').Default("false").OverrideDefaultFromEnvar("LEADER_ELECTION").Bool()
		leaderRenew    = app.Flag("leader-renew", "Set leader election renewal.").Short('r').Default("10s").OverrideDefaultFromEnvar("LEADER_ELECTION_RENEW").Duration()

		syncInterval            = app.Flag("sync", "How often all resources will be double-checked for drift from the desired state.").Short('s').Default("1h").Duration()
		backendMonitorInterval  = app.Flag("backend-monitor-interval", "Interval between backend monitor controller reconciliations.").Default("60s").Duration()
		pollInterval            = app.Flag("poll", "How often individual resources will be checked for drift from the desired state").Short('p').Default("30m").Duration()
		pollStateMetricInterval = app.Flag("poll-state-metric", "State metric recording interval").Default("5s").Duration()
		reconcileConcurrency    = app.Flag("reconcile-concurrency", "Set number of reconciliation loops.").Default("100").Int()
		maxReconcileRate        = app.Flag("max-reconcile-rate", "The global maximum rate per second at which resources may checked for drift from the desired state.").Default("1000").Int()
		reconcileTimeout        = app.Flag("reconcile-timeout", "Object reconciliation timeout").Short('t').Default("3s").Duration()
		s3Timeout               = app.Flag("s3-timeout", "S3 API operations timeout").Default("10s").Duration()
		creationGracePeriod     = app.Flag("creation-grace-period", "Duration to wait for the external API to report that a newly created external resource exists.").Default("10s").Duration()
		tracesEnabled           = app.Flag("otel-enable-tracing", "").Default("false").Bool()
		tracesExportTimeout     = app.Flag("otel-traces-export-timeout", "Timeout when exporting traces").Default("2s").Duration()
		tracesExportInterval    = app.Flag("otel-traces-export-interval", "Interval at which traces are exported").Default("5s").Duration()
		tracesExportAddress     = app.Flag("otel-traces-export-address", "Address of otel collector").Default("opentelemetry-collector.opentelemetry:4317").String()

		kubeClientRate       = app.Flag("kube-client-rate", "The global maximum rate per second at how many requests the client can do.").Default("1000").Int()
		kubeClientTimeout    = app.Flag("kube-client-timeout", "Kube client request timeout.").Default("10s").Duration()
		kubeCacheSyncTimeout = app.Flag("cache-sync-timeout", "Kube client cache sync timeout.").Default("60s").Duration()

		namespace                  = app.Flag("namespace", "Namespace used to set as default scope in default secret store config.").Default("crossplane-system").Envar("POD_NAMESPACE").String()
		enableExternalSecretStores = app.Flag("enable-external-secret-stores", "Enable support for ExternalSecretStores.").Default("false").Envar("ENABLE_EXTERNAL_SECRET_STORES").Bool()
		enableManagementPolicies   = app.Flag("enable-management-policies", "Enable support for Management Policies.").Default("false").Envar("ENABLE_MANAGEMENT_POLICIES").Bool()

		autoPauseBucket       = app.Flag("auto-pause-bucket", "Enable auto pause of reconciliation of ready buckets").Default("false").Envar("AUTO_PAUSE_BUCKET").Bool()
		minReplicas           = app.Flag("minimum-replicas", "Minimum number of replicas of a bucket before it is considered Ready").Default("1").Envar("MINIMUM_REPLICAS").Uint()
		recreateMissingBucket = app.Flag("recreate-missing-bucket", "Recreates existing bucket if missing").Default("true").Envar("RECREATE_MISSING_BUCKET").Bool()

		assumeRoleArn = app.Flag("assume-role-arn", "Assume role ARN to be used for STS authentication").Default("").Envar("ASSUME_ROLE_ARN").String()

		webhookHost       = app.Flag("webhook-host", "The host of the webhook server.").Default("0.0.0.0").Envar("WEBHOOK_HOST").String()
		webhookTLSCertDir = app.Flag("webhook-tls-cert-dir", "The directory of TLS certificate that will be used by the webhook server. There should be tls.crt and tls.key files.").Default("/").Envar("WEBHOOK_TLS_CERT_DIR").String()
		_                 = app.Flag("enable-validation-webhooks", "Enable support for Webhooks. [Deprecated, has no effect]").Default("false").Bool()
		// Subresource Client Flags.
		disableACLReconcile              = app.Flag("disable-acl-reconcile", "Disable reconciliation of Bucket ACLs.").Default("false").Envar("DISABLE_ACL_RECONCILE").Bool()
		disablePolicyReconcile           = app.Flag("disable-policy-reconcile", "Disable reconciliation of Bucket Policies.").Default("false").Envar("DISABLE_POLICY_RECONCILE").Bool()
		disableLifecycleConfigReconcile  = app.Flag("disable-lifecycle-config-reconcile", "Disable reconciliation of Bucket Lifecycle Configurations.").Default("false").Envar("DISABLE_LIFECYCLE_CONFIG_RECONCILE").Bool()
		disableVersioningConfigReconcile = app.Flag("disable-versioning-config-reconcile", "Disable reconciliation of Bucket Versioning Configurations.").Default("false").Envar("DISABLE_VERSIONING_CONFIG_RECONCILE").Bool()
		disableObjectLockConfigReconcile = app.Flag("disable-object-lock-config-reconcile", "Disable reconciliation of Object Lock Configurations.").Default("false").Envar("DISABLE_OBJECT_LOCK_CONFIG_RECONCILE").Bool()
	)

	var zo zap.Options
	var zapDevel *bool

	zapFlagSet := flag.NewFlagSet(os.Args[0], flag.ContinueOnError)
	zo.BindFlags(zapFlagSet)

	zapOpts := []zap.Opts{}
	zapFlagSet.VisitAll(func(f *flag.Flag) {
		defaultValue, ok := defaultZapConfig[f.Name]
		if !ok {
			defaultValue = f.DefValue
		}
		kf := app.Flag(f.Name, f.Usage).Default(defaultValue)

		switch f.Name {
		case "zap-devel":
			// Store the value for zap-devel for use when we come to zap-log-level so that we
			// do not overwrite the level. VisitAll visits flags in lexicographical order so it
			// is safe to assume "zap-devel" will always be visited before "zap-log-level".
			zapDevel = kf.Bool()
			zapOpts = append(zapOpts, func(o *zap.Options) {
				o.Development = *zapDevel
			})
		case "zap-encoder":
			e := kf.String()
			zapOpts = append(zapOpts, func(o *zap.Options) {
				o.NewEncoder = func(eco ...zap.EncoderConfigOption) zapcore.Encoder {
					if *e == "json" {
						zap.JSONEncoder(eco...)(o)
					} else {
						zap.ConsoleEncoder(eco...)(o)
					}

					return o.Encoder
				}
			})
		case "zap-log-level":
			ll := kf.String()
			zapOpts = append(zapOpts, func(o *zap.Options) {
				l := zapcore.Level(0)
				app.FatalIfError(l.Set(*ll), "Unable to unmarshal zap-log-level")
				// if zap-devel is enabled, the log level should be debug (-1).
				if *zapDevel {
					l = zapcore.Level(-1)
				}
				o.Level = l
			})
		case "zap-stacktrace-level":
			sl := kf.String()
			zapOpts = append(zapOpts, func(o *zap.Options) {
				l := zapcore.Level(0)
				app.FatalIfError(l.Set(*sl), "Unable to unmarshal zap-stacktrace-level")
				o.StacktraceLevel = l
			})
		case "zap-time-encoding":
			te := kf.String()
			zapOpts = append(zapOpts, func(o *zap.Options) {
				o.TimeEncoder = zapcore.EpochTimeEncoder
				app.FatalIfError(o.TimeEncoder.UnmarshalText([]byte(*te)), "Unable to unmarshal zap-time-encoding")
			})
		}
	})

	kingpin.MustParse(app.Parse(os.Args[1:]))

	log := zap.New(zapOpts...).WithName("provider-ceph")
	ctrl.SetLogger(log)
	klog.SetLogger(log)

	// Init otel tracer provider if the user sets the flag
	if *tracesEnabled {
		flush, err := traces.InitTracerProvider(log, *tracesExportAddress, *tracesExportTimeout, *tracesExportInterval)
		kingpin.FatalIfError(err, "Cannot start tracer provider")

		// overwrite the default terminate function called on FatalIfError()
		app.Terminate(func(i int) {
			// default behavior
			defer os.Exit(i)

			// flush traces
			ctx, cancel := context.WithTimeout(context.Background(), *tracesExportTimeout)
			defer cancel()

			flush(ctx)
		})
	}

	cfg, err := ctrl.GetConfig()
	kingpin.FatalIfError(err, "Cannot get API server rest config")

	cfg = ratelimiter.LimitRESTConfig(cfg, *kubeClientRate)

	const oneDotTwo = 1.2
	const two = 2

	leaseDuration := time.Duration(int(oneDotTwo*float64(*leaderRenew))) * time.Second
	leaderRetryDuration := *leaderRenew / two

	pausedSelector, err := labels.NewRequirement(meta.AnnotationKeyReconciliationPaused, selection.NotIn, []string{"true"})
	kingpin.FatalIfError(err, "Cannot create label selector")

	providerScheme := scheme.Scheme
	kingpin.FatalIfError(apis.AddToScheme(providerScheme), "Cannot add Ceph APIs to scheme")

	httpClient, err := rest.HTTPClientFor(cfg)
	kingpin.FatalIfError(err, "Cannot create HTTP client")

	httpClient.Transport = otelhttp.NewTransport(httpClient.Transport)
	httpClient.Timeout = *kubeClientTimeout

	mm := managed.NewMRMetricRecorder()
	sm := statemetrics.NewMRStateMetrics()

	metrics.Registry.MustRegister(mm)
	metrics.Registry.MustRegister(sm)

	mo := controller.MetricOptions{
		PollStateMetricInterval: *pollStateMetricInterval,
		MRMetrics:               mm,
		MRStateMetrics:          sm,
	}

	mgr, err := ctrl.NewManager(cfg, ctrl.Options{
		LeaderElection:             *leaderElection,
		LeaderElectionID:           "crossplane-leader-election-provider-ceph-ibyaiby",
		LeaderElectionNamespace:    *namespace,
		LeaderElectionResourceLock: resourcelock.LeasesResourceLock,
		RenewDeadline:              leaderRenew,
		LeaseDuration:              &leaseDuration,
		RetryPeriod:                &leaderRetryDuration,
		WebhookServer: webhook.NewServer(webhook.Options{
			Host:    *webhookHost,
			CertDir: *webhookTLSCertDir,
		}),
		Scheme: providerScheme,
		Cache: cache.Options{
			HTTPClient: httpClient,
			SyncPeriod: syncInterval,
			Scheme:     providerScheme,
			ByObject: map[client.Object]cache.ByObject{
				&providercephv1alpha1.Bucket{}: {
					Label: labels.NewSelector().Add(*pausedSelector),
				},
				&v1alpha1.ProviderConfig{}: {},
			},
		},
		NewCache: cache.New,
	})
	kingpin.FatalIfError(err, "Cannot create controller manager")

	o := controller.Options{
		Logger:                  logging.NewLogrLogger(log),
		MaxConcurrentReconciles: *reconcileConcurrency,
		PollInterval:            *pollInterval,
		GlobalRateLimiter:       ratelimiter.NewGlobal(*maxReconcileRate),
		Features:                &feature.Flags{},
		MetricOptions:           &mo,
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

	if *enableManagementPolicies {
		o.Features.Enable(features.EnableAlphaManagementPolicies)
		log.Info("Alpha feature enabled", "flag", features.EnableAlphaManagementPolicies)
	}

	backendStore := backendstore.NewBackendStore()

	// Create a cached reader for use in the health check controller when performing List Buckets.
	cachedReader, err := newCachedReader(cfg, providerScheme, *kubeCacheSyncTimeout, log)
	kingpin.FatalIfError(err, "Cannot setup cached reader")

	kingpin.FatalIfError(ctrl.NewWebhookManagedBy(mgr).
		For(&providercephv1alpha1.Bucket{}).
		WithValidator(bucket.NewBucketValidator(backendStore)).
		Complete(), "Cannot setup bucket validating webhook")

	kingpin.FatalIfError(providerconfig.Setup(mgr, o,
		backendmonitor.NewController(
			backendmonitor.WithKubeClient(mgr.GetClient()),
			backendmonitor.WithBackendStore(backendStore),
			backendmonitor.WithS3Timeout(*s3Timeout),
			backendmonitor.WithRequeueInterval(*backendMonitorInterval),
			backendmonitor.WithLogger(log)),
		healthcheck.NewController(
			healthcheck.WithAutoPause(autoPauseBucket),
			healthcheck.WithBackendStore(backendStore),
			healthcheck.WithCachedReader(cachedReader),
			healthcheck.WithKubeClient(mgr.GetClient()),
			healthcheck.WithHttpClient(&http.Client{Timeout: *s3Timeout}),
			healthcheck.WithLogger(log))),
		"Cannot setup ProviderConfig controllers")

	s3ClientHandler := s3clienthandler.NewHandler(
		s3clienthandler.WithAssumeRoleArn(assumeRoleArn),
		s3clienthandler.WithBackendStore(backendStore),
		s3clienthandler.WithKubeClient(mgr.GetClient()),
		s3clienthandler.WithS3Timeout(*s3Timeout),
		s3clienthandler.WithLog(log))

	kingpin.FatalIfError(bucket.Setup(mgr, o, bucket.NewConnector(
		bucket.WithAutoPause(autoPauseBucket),
		bucket.WithMinimumReplicas(minReplicas),
		bucket.WithRecreateMissingBucket(recreateMissingBucket),
		bucket.WithBackendStore(backendStore),
		bucket.WithKubeClient(mgr.GetClient()),
		bucket.WithOperationTimeout(*reconcileTimeout),
		bucket.WithCreationGracePeriod(*creationGracePeriod),
		bucket.WithPollInterval(*pollInterval),
		bucket.WithLog(log),
		bucket.WithSubresourceClients(
			bucket.NewSubresourceClients(
				backendStore,
				s3ClientHandler,
				bucket.SubresourceClientConfig{
					LifecycleConfigurationClientDisabled:  *disableLifecycleConfigReconcile,
					ACLClientDisabled:                     *disableACLReconcile,
					PolicyClientDisabled:                  *disablePolicyReconcile,
					VersioningConfigurationClientDisabled: *disableVersioningConfigReconcile,
					ObjectLockConfigurationClientDisabled: *disableObjectLockConfigReconcile},
				log)),
		bucket.WithS3ClientHandler(s3ClientHandler),
		bucket.WithUsage(resource.NewProviderConfigUsageTracker(mgr.GetClient(), &v1alpha1.ProviderConfigUsage{})),
		bucket.WithNewServiceFn(bucket.NewNoOpService))), "Cannot setup Bucket controller")

	kingpin.FatalIfError(mgr.Start(ctrl.SetupSignalHandler()), "Cannot start controller manager")
}

func newCachedReader(cfg *rest.Config, s *runtime.Scheme, syncTimeout time.Duration, l logr.Logger) (client.Reader, error) {
	informerCache, err := cache.New(cfg, cache.Options{
		Scheme: s,
	})
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), syncTimeout)
	defer cancel()

	go func() {
		if err := informerCache.Start(ctx); err != nil {
			l.Error(err, "failed to start informer for cached reader")
		}
	}()

	if !informerCache.WaitForCacheSync(ctx) {
		return nil, errFailedToSyncCache
	}
	var cachedReader client.Reader = informerCache

	return cachedReader, nil
}
