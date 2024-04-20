/*
Copyright AppsCode Inc. and Contributors.

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

package agent

import (
	"context"
	"flag"
	"os"

	authenticationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authentication/v1alpha1"
	authorizationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authorization/v1alpha1"
	"github.com/kluster-manager/cluster-auth/pkg/agent/controller"

	"github.com/spf13/cobra"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/klog/v2"
	cu "kmodules.xyz/client-go/client"
	ocmoperator "open-cluster-management.io/api/operator/v1"
	managedsaapi "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/cache"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
	"sigs.k8s.io/controller-runtime/pkg/manager"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))
	utilruntime.Must(rbac.AddToScheme(scheme))

	utilruntime.Must(authenticationv1alpha1.AddToScheme(scheme))
	utilruntime.Must(authorizationv1alpha1.AddToScheme(scheme))
	utilruntime.Must(managedsaapi.AddToScheme(scheme))
	utilruntime.Must(ocmoperator.Install(scheme))
}

func NewCmdAgent() *cobra.Command {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var hubKC string
	opts := zap.Options{
		Development: true,
	}

	cmd := &cobra.Command{
		Use:               "agent",
		Short:             "Launch cluster auth addon agent",
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

			spokeCfg := ctrl.GetConfigOrDie()
			mgr, err := ctrl.NewManager(spokeCfg, ctrl.Options{
				Scheme:                 scheme,
				Metrics:                metricsserver.Options{BindAddress: metricsAddr},
				HealthProbeBindAddress: probeAddr,
				LeaderElection:         enableLeaderElection,
				LeaderElectionID:       "cluster-auth-agent",
			})
			if err != nil {
				setupLog.Error(err, "unable to start manager")
				os.Exit(1)
			}

			// get hub kubeconfig
			hubConfig, err := clientcmd.BuildConfigFromFlags("", hubKC)
			if err != nil {
				klog.Fatalf("unable to build hub rest config: %v", err)
				os.Exit(1)
			}

			// get klusterlet
			kl := ocmoperator.Klusterlet{}
			err = mgr.GetAPIReader().Get(context.Background(), client.ObjectKey{Name: "klusterlet"}, &kl)
			if err != nil {
				klog.Fatalf("unable to get klusterlet: %v", err)
				os.Exit(1)
			}

			hubManager, err := manager.New(hubConfig, manager.Options{
				Scheme:                 scheme,
				Metrics:                metricsserver.Options{BindAddress: "0"},
				HealthProbeBindAddress: "",
				NewClient:              cu.NewClient,
				Cache: cache.Options{
					ByObject: map[client.Object]cache.ByObject{
						&authorizationv1alpha1.ManagedClusterRoleBinding{}: {
							Namespaces: map[string]cache.Config{
								kl.Spec.ClusterName: {},
							},
						},
					},
				},
			})
			if err != nil {
				setupLog.Error(err, "unable to start hub manager")
				os.Exit(1)
			}

			if err := (&controller.ManagedClusterRoleBindingReconciler{
				HubClient:   hubManager.GetClient(),
				SpokeClient: mgr.GetClient(),
			}).SetupWithManager(hubManager); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "ManagedClusterRoleBindingReconciler")
				os.Exit(1)
			}

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

			err = mgr.Add(manager.RunnableFunc(func(ctx context.Context) error {
				setupLog.Info("starting hub manager")
				return hubManager.Start(ctx)
			}))
			if err != nil {
				setupLog.Error(err, "problem running hub manager")
				os.Exit(1)
			}

			setupLog.Info("starting manager")
			if err := mgr.Start(ctx); err != nil {
				klog.Fatalf("unable to start controller manager: %v", err)
				os.Exit(1)
			}
		},
	}

	flag.StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	flag.StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	flag.BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	flag.StringVar(&hubKC, "hub-kubeconfig", "", "Path to hub kubeconfig")

	fs := flag.NewFlagSet("zap", flag.ExitOnError)
	opts.BindFlags(fs)
	cmd.Flags().AddGoFlagSet(fs)

	return cmd
}
