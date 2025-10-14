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

//+kubebuilder:rbac:groups=apiextensions.k8s.io,resources=customresourcedefinitions,verbs=get;list;watch

import (
	"context"
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
	authv1 "k8s.io/api/authorization/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/selection"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/leaderelection/resourcelock"
	"k8s.io/klog/v2"
	ctrl "sigs.k8s.io/controller-runtime"
	kcache "sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	"sigs.k8s.io/controller-runtime/pkg/metrics"
	"sigs.k8s.io/controller-runtime/pkg/webhook"

	"github.com/crossplane/crossplane-runtime/v2/pkg/controller"
	"github.com/crossplane/crossplane-runtime/v2/pkg/errors"
	"github.com/crossplane/crossplane-runtime/v2/pkg/feature"
	"github.com/crossplane/crossplane-runtime/v2/pkg/gate"
	"github.com/crossplane/crossplane-runtime/v2/pkg/logging"
	"github.com/crossplane/crossplane-runtime/v2/pkg/meta"
	"github.com/crossplane/crossplane-runtime/v2/pkg/ratelimiter"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/customresourcesgate"
	"github.com/crossplane/crossplane-runtime/v2/pkg/reconciler/managed"
	"github.com/crossplane/crossplane-runtime/v2/pkg/resource"
	"github.com/crossplane/crossplane-runtime/v2/pkg/statemetrics"

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

// canWatchCRD checks if the provider has the necessary RBAC permissions to
// watch CustomResourceDefinitions. This is required for safe-start to function.
func canWatchCRD(ctx context.Context, mgr manager.Manager) (bool, error) {
	if err := authv1.AddToScheme(mgr.GetScheme()); err != nil {
		return false, err
	}
	verbs := []string{"get", "list", "watch"}
	for _, verb := range verbs {
		sar := &authv1.SelfSubjectAccessReview{
			Spec: authv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authv1.ResourceAttributes{
					Group:    "apiextensions.k8s.io",
					Resource: "customresourcedefinitions",
					Verb:     verb,
				},
			},
		}
		if err := mgr.GetClient().Create(ctx, sar); err != nil {
			return false, errors.Wrapf(err, "unable to perform RBAC check for verb %s on CustomResourceDefinitions", verb)
		}
		if !sar.Status.Allowed {
			return false, nil
		}
	}

	return true, nil
}

// setupZapLogging configures zap logging flags and returns configured options.
func setupZapLogging(app *kingpin.Application, debugFlag *bool) []zap.Opts {
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
				if *zapDevel || *debugFlag {
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

	return zapOpts
}

// configureSafeStart configures the safe-start gate if permissions allow.
func configureSafeStart(o *controller.Options, canSafeStart bool) {
	if canSafeStart {
		o.Gate = new(gate.Gate[schema.GroupVersionKind])
	}
}

// configureManagementPolicies enables management policies if requested.
func configureManagementPolicies(o *controller.Options, enableManagementPolicies bool, log logr.Logger) {
	if enableManagementPolicies {
		o.Features.Enable(features.EnableAlphaManagementPolicies)
		log.Info("Alpha feature enabled", "flag", features.EnableAlphaManagementPolicies)
	}
}

// createBucketConnector creates a bucket connector with all required options.
func createBucketConnector(
	mgr manager.Manager,
	backendStore *backendstore.BackendStore,
	s3ClientHandler *s3clienthandler.Handler,
	log logr.Logger,
	autoPauseBucket *bool,
	minReplicas *uint,
	recreateMissingBucket *bool,
	reconcileTimeout *time.Duration,
	creationGracePeriod *time.Duration,
	pollInterval *time.Duration,
	disableACLReconcile *bool,
	disablePolicyReconcile *bool,
	disableLifecycleConfigReconcile *bool,
	disableVersioningConfigReconcile *bool,
	disableObjectLockConfigReconcile *bool,
) *bucket.Connector {
	return bucket.NewConnector(
		bucket.WithAutoPause(autoPauseBucket),
		bucket.WithMinimumReplicas(minReplicas),
		bucket.WithRecreateMissingBucket(recreateMissingBucket),
		bucket.WithBackendStore(backendStore),
		bucket.WithKubeClient(mgr.GetClient()),
		bucket.WithKubeReader(mgr.GetAPIReader()),
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
		bucket.WithUsage(resource.NewLegacyProviderConfigUsageTracker(mgr.GetClient(), &v1alpha1.ProviderConfigUsage{})),
		bucket.WithNewServiceFn(bucket.NewNoOpService))
}

// setupControllers sets up bucket controllers with or without safe-start gating.
func setupControllers(mgr manager.Manager, o controller.Options, connector *bucket.Connector, canSafeStart bool, log logr.Logger) {
	if canSafeStart {
		// Setup the CRD gate controller first
		kingpin.FatalIfError(customresourcesgate.Setup(mgr, o), "Cannot setup CRD gate")
		// Setup controllers with gated versions
		kingpin.FatalIfError(bucket.SetupGated(mgr, o, connector), "Cannot setup gated Bucket controller")
	} else {
		log.Info("Provider has missing RBAC permissions for watching CRDs, controller SafeStart capability will be disabled")
		// Setup controllers directly without gating
		kingpin.FatalIfError(bucket.Setup(mgr, o, connector), "Cannot setup Bucket controller")
	}
}

// setupBucketWebhook sets up the bucket validating webhook.
func setupBucketWebhook(mgr manager.Manager, backendStore *backendstore.BackendStore) {
	kingpin.FatalIfError(ctrl.NewWebhookManagedBy(mgr).
		For(&providercephv1alpha1.Bucket{}).
		WithValidator(bucket.NewBucketValidator(backendStore)).
		Complete(), "Cannot setup bucket validating webhook")
}

// setupProviderConfigControllers sets up the provider config, backend monitor, and health check controllers.
func setupProviderConfigControllers(
	mgr manager.Manager,
	o controller.Options,
	backendStore *backendstore.BackendStore,
	kubeClientUncached client.Client,
	log logr.Logger,
	s3Timeout time.Duration,
	backendMonitorInterval time.Duration,
	autoPauseBucket *bool,
) {
	kingpin.FatalIfError(providerconfig.Setup(mgr, o,
		backendmonitor.NewController(
			backendmonitor.WithKubeClient(mgr.GetClient()),
			backendmonitor.WithBackendStore(backendStore),
			backendmonitor.WithS3Timeout(s3Timeout),
			backendmonitor.WithRequeueInterval(backendMonitorInterval),
			backendmonitor.WithLogger(log)),
		healthcheck.NewController(
			healthcheck.WithAutoPause(autoPauseBucket),
			healthcheck.WithBackendStore(backendStore),
			healthcheck.WithKubeClientUncached(kubeClientUncached),
			healthcheck.WithKubeClientCached(mgr.GetClient()),
			healthcheck.WithHttpClient(&http.Client{Timeout: s3Timeout}),
			healthcheck.WithLogger(log))),
		"Cannot setup ProviderConfig controllers")
}

// createS3ClientHandler creates an S3 client handler with all required options.
func createS3ClientHandler(
	assumeRoleArn *string,
	backendStore *backendstore.BackendStore,
	kubeClient client.Client,
	s3Timeout time.Duration,
	log logr.Logger,
) *s3clienthandler.Handler {
	return s3clienthandler.NewHandler(
		s3clienthandler.WithAssumeRoleArn(assumeRoleArn),
		s3clienthandler.WithBackendStore(backendStore),
		s3clienthandler.WithKubeClient(kubeClient),
		s3clienthandler.WithS3Timeout(s3Timeout),
		s3clienthandler.WithLog(log))
}

func main() {
	var (
		app            = kingpin.New(filepath.Base(os.Args[0]), "Ceph support for Crossplane.").DefaultEnvars()
		leaderElection = app.Flag("leader-election", "Use leader election for the controller manager.").Short('l').Default("false").OverrideDefaultFromEnvar("LEADER_ELECTION").Bool()
		leaderRenew    = app.Flag("leader-renew", "Set leader election renewal.").Short('r').Default("10s").OverrideDefaultFromEnvar("LEADER_ELECTION_RENEW").Duration()

		syncInterval            = app.Flag("sync", "How often all resources will be double-checked for drift from the desired state.").Short('s').Default("1h").Duration()
		syncTimeout             = app.Flag("sync-timeout", "Cache sync timeout.").Default("10s").Duration()
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

		kubeClientRate = app.Flag("kube-client-rate", "The global maximum rate per second at how many requests the client can do.").Default("1000").Int()

		namespace                = app.Flag("namespace", "Namespace used to set as default scope in default secret store config.").Default("crossplane-system").Envar("POD_NAMESPACE").String()
		enableManagementPolicies = app.Flag("enable-management-policies", "Enable support for Management Policies.").Default("false").Envar("ENABLE_MANAGEMENT_POLICIES").Bool()

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

	debugFlag := app.Flag("debug", "Enable debug logging (sets zap-log-level to debug)").Default("false").Bool()
	zapOpts := setupZapLogging(app, debugFlag)

	kingpin.MustParse(app.Parse(os.Args[1:]))

	log := zap.New(zapOpts...).WithName("provider-ceph")
	ctrl.SetLogger(log)
	klog.SetLogger(log)

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
	httpClient.Timeout = *syncTimeout

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
		Cache: kcache.Options{
			HTTPClient: httpClient,
			SyncPeriod: syncInterval,
			Scheme:     providerScheme,
			ByObject: map[client.Object]kcache.ByObject{
				&providercephv1alpha1.Bucket{}: {
					Label: labels.NewSelector().Add(*pausedSelector),
				},
				&v1alpha1.ProviderConfig{}: {},
			},
		},
		NewCache: kcache.New,
	})
	kingpin.FatalIfError(err, "Cannot create controller manager")

	ctx := context.Background()

	kingpin.FatalIfError(apiextensionsv1.AddToScheme(mgr.GetScheme()), "Cannot add apiextensionsv1 to scheme")

	// Check if provider has permissions to watch CRDs for safe-start capability
	canSafeStart, err := canWatchCRD(ctx, mgr)
	kingpin.FatalIfError(err, "SafeStart precheck failed")

	o := controller.Options{
		Logger:                  logging.NewLogrLogger(log),
		MaxConcurrentReconciles: *reconcileConcurrency,
		PollInterval:            *pollInterval,
		GlobalRateLimiter:       ratelimiter.NewGlobal(*maxReconcileRate),
		Features:                &feature.Flags{},
		MetricOptions:           &mo,
	}

	configureSafeStart(&o, canSafeStart)
	configureManagementPolicies(&o, *enableManagementPolicies, log)

	backendStore := backendstore.NewBackendStore()

	kubeClientUncached, err := client.New(cfg, client.Options{
		Scheme: providerScheme,
		HTTPClient: &http.Client{
			Transport: httpClient.Transport,
		},
	})
	kingpin.FatalIfError(err, "Cannot create Kube client")

	setupBucketWebhook(mgr, backendStore)
	setupProviderConfigControllers(mgr, o, backendStore, kubeClientUncached, log, *s3Timeout, *backendMonitorInterval, autoPauseBucket)
	s3ClientHandler := createS3ClientHandler(assumeRoleArn, backendStore, mgr.GetClient(), *s3Timeout, log)

	connector := createBucketConnector(mgr, backendStore, s3ClientHandler, log, autoPauseBucket, minReplicas,
		recreateMissingBucket, reconcileTimeout, creationGracePeriod, pollInterval, disableACLReconcile,
		disablePolicyReconcile, disableLifecycleConfigReconcile, disableVersioningConfigReconcile, disableObjectLockConfigReconcile)

	setupControllers(mgr, o, connector, canSafeStart, log)

	kingpin.FatalIfError(mgr.Start(ctrl.SetupSignalHandler()), "Cannot start controller manager")
}
