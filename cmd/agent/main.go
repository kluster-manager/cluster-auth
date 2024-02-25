/*
Copyright 2024.

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
	"github.com/kluster-manager/cluster-auth/pkg/addon/agent/controller"
	"github.com/kluster-manager/cluster-auth/pkg/util"
	v1 "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/clientcmd/api"
	"k8s.io/klog/v2"
	cg "kmodules.xyz/client-go/client"
	ocmkl "open-cluster-management.io/api/operator/v1"
	mSA "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1alpha1"
	"os"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	_ "k8s.io/client-go/plugin/pkg/client/auth"

	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	authenticationv1alpha1 "github.com/kluster-manager/cluster-auth/api/authentication/v1alpha1"
	authorizationv1alpha1 "github.com/kluster-manager/cluster-auth/api/authorization/v1alpha1"
	//+kubebuilder:scaffold:imports
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

const (
	HubKubeConfigSecretName      = "cluster-auth-hub-kubeconfig"
	HubKubeConfigSecretNamespace = "cluster-auth"
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(authenticationv1alpha1.AddToScheme(scheme))
	utilruntime.Must(authorizationv1alpha1.AddToScheme(scheme))
	utilruntime.Must(rbac.AddToScheme(scheme))
	utilruntime.Must(mSA.AddToScheme(scheme))
	//+kubebuilder:scaffold:scheme
}

func GetKubeClient(kubeConfig *rest.Config) (client.Client, error) {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = authenticationv1alpha1.AddToScheme(scheme)
	_ = authorizationv1alpha1.AddToScheme(scheme)
	return client.New(kubeConfig, client.Options{Scheme: scheme})
}

func main() {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var clusterName string
	var spokeKubeconfig string
	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	flag.StringVar(&clusterName, "cluster-name", "", "The name of the managed cluster.")
	flag.StringVar(&spokeKubeconfig, "spoke-kubeconfig", "", "The kubeconfig to talk to the managed cluster, "+
		"will use the in-cluster client if not specified.")
	opts := zap.Options{
		Development: true,
	}
	opts.BindFlags(flag.CommandLine)
	flag.Parse()

	ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

	if len(clusterName) == 0 {
		klog.Fatal("missing --cluster-name")
	}

	spokeCfg, err := rest.InClusterConfig()
	if err != nil {
		klog.Fatal("failed build a in-cluster spoke cluster client config")
	}

	mgr, err := ctrl.NewManager(spokeCfg, ctrl.Options{
		Scheme:                 scheme,
		Metrics:                metricsserver.Options{BindAddress: metricsAddr},
		HealthProbeBindAddress: probeAddr,
		LeaderElection:         enableLeaderElection,
		LeaderElectionID:       "cluster-auth-agent",
		// LeaderElectionReleaseOnCancel defines if the leader should step down voluntarily
		// when the Manager ends. This requires the binary to immediately end when the
		// Manager is stopped, otherwise, this setting is unsafe. Setting this significantly
		// speeds up voluntary leader transitions as the new leader don't have to wait
		// LeaseDuration time first.
		//
		// In the default scaffold provided, the program ends immediately after
		// the manager stops, so would be fine to enable this option. However,
		// if you are doing or is intended to do any operation such as perform cleanups
		// after the manager stops then its usage might be unsafe.
		// LeaderElectionReleaseOnCancel: true,
	})
	if err != nil {
		setupLog.Error(err, "unable to start manager")
		os.Exit(1)
	}

	hubNativeClient, err := kubernetes.NewForConfig(mgr.GetConfig())
	if err != nil {
		klog.Fatal("unable to instantiate a kubernetes native client")
	}

	spokeNativeClient, err := kubernetes.NewForConfig(spokeCfg)
	if err != nil {
		klog.Fatal("unable to build a spoke kubernetes client")
	}

	spokeNamespace := os.Getenv("NAMESPACE")
	if len(spokeNamespace) == 0 {
		inClusterNamespace, err := util.GetInClusterNamespace()
		if err != nil {
			klog.Fatal("the agent should be either running in a container or specify NAMESPACE environment")
		}
		spokeNamespace = inClusterNamespace
	}
	sc, err := util.GetKubeClient(spokeCfg)
	if err != nil {
		klog.Fatal("-1")
	}
	// get hub kubeconfig from secret
	s := v1.Secret{}
	err = sc.Get(context.Background(), client.ObjectKey{Name: HubKubeConfigSecretName, Namespace: HubKubeConfigSecretNamespace}, &s)
	if err != nil {
		klog.Fatal("")
	}

	konfig, err := clientcmd.NewClientConfigFromBytes(s.Data["kubeconfig"])
	if err != nil {
		klog.Fatal("")
	}

	apiConfig, err := konfig.RawConfig()
	if err != nil {
		klog.Fatal("")
	}

	authInfo := apiConfig.Contexts[apiConfig.CurrentContext].AuthInfo
	apiConfig.AuthInfos[authInfo] = &api.AuthInfo{
		ClientCertificateData: s.Data["tls.crt"],
		ClientKeyData:         s.Data["tls.key"],
	}

	konfig = clientcmd.NewNonInteractiveClientConfig(apiConfig, apiConfig.CurrentContext, &clientcmd.ConfigOverrides{}, nil)
	// hub restConfig
	restConfig, err := konfig.ClientConfig()
	if err != nil {
		klog.Fatal("")
	}

	c, err := util.GetKubeClient(restConfig)
	if err != nil {
		klog.Fatal("")
	}

	// get klusterlet
	kl := ocmkl.Klusterlet{}
	err = sc.Get(context.Background(), client.ObjectKey{Name: "klusterlet"}, &kl)
	if err != nil {
		klog.Fatal("")
	}

	if err = (&controller.ClusterRoleBindingReconciler{
		HubClient:         c,
		HubNativeClient:   hubNativeClient,
		SpokeNamespace:    spokeNamespace,
		SpokeClientConfig: spokeCfg,
		SpokeNativeClient: spokeNativeClient,
		ClusterName:       kl.Spec.ClusterName,
	}).SetupWithManager(mgr); err != nil {
		klog.Fatalf("unable to create controller %v", "ManagedServiceAccount")
	}
	//+kubebuilder:scaffold:builder

	ctx, cancel := context.WithCancel(ctrl.SetupSignalHandler())
	defer cancel()

	if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up health check")
		os.Exit(1)
	}
	if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
		setupLog.Error(err, "unable to set up ready check")
		os.Exit(1)
	}

	setupLog.Info("starting manager")
	if err := mgr.Start(ctx); err != nil {
		klog.Fatalf("unable to start controller manager: %v", err)
	}

	// aggregate klusterlet-work permission
	cr := &rbac.ClusterRole{
		ObjectMeta: ctrl.ObjectMeta{
			Name: "open-cluster-management:klusterlet-work:cluster-auth",
			Labels: map[string]string{
				"open-cluster-management.io/aggregate-to-work": "true",
			},
		},
		Rules: []rbac.PolicyRule{
			{
				APIGroups: []string{"*"},
				Resources: []string{"*"},
				Verbs:     []string{"*"},
			},
		},
	}
	spokeClient, err := util.GetKubeClient(spokeCfg)
	if err != nil {
		klog.Fatalf("unable to start controller manager: %v", err)
	}
	_, err = cg.CreateOrPatch(context.Background(), spokeClient, cr, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*rbac.ClusterRole)
		in.Rules = cr.Rules
		return in
	})
	if err != nil {
		klog.Fatalf("unable to start controller manager: %v", err)
	}
}

func getHubKubeconfigPath() string {
	hubKubeconfigPath := os.Getenv("HUB_KUBECONFIG")
	if len(hubKubeconfigPath) == 0 {
		hubKubeconfigPath = "/etc/hub/kubeconfig"
	}
	return hubKubeconfigPath
}
