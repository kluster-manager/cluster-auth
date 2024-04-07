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

package manager

import (
	"context"
	"flag"
	"os"

	authenticationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authentication/v1alpha1"
	authorizationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authorization/v1alpha1"
	"github.com/kluster-manager/cluster-auth/pkg/common"
	"github.com/kluster-manager/cluster-auth/pkg/manager"
	"github.com/kluster-manager/cluster-auth/pkg/manager/controller/authorization"

	monitoringv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	"github.com/spf13/cobra"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	"open-cluster-management.io/addon-framework/pkg/utils"
	"open-cluster-management.io/api/addon/v1alpha1"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	workv1 "open-cluster-management.io/api/work/v1"
	managedsaapi "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
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
	utilruntime.Must(clusterv1.Install(scheme))
	utilruntime.Must(clusterv1beta2.Install(scheme))
	utilruntime.Must(workv1.Install(scheme))
	utilruntime.Must(managedsaapi.AddToScheme(scheme))
	utilruntime.Must(monitoringv1.AddToScheme(scheme))
}

func NewCmdManager() *cobra.Command {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var registryFQDN string
	opts := zap.Options{
		Development: true,
	}

	cmd := &cobra.Command{
		Use:               "manager",
		Short:             "Launch cluster auth addon manager",
		DisableAutoGenTag: true,
		Run: func(cmd *cobra.Command, args []string) {
			ctrl.SetLogger(zap.New(zap.UseFlagOptions(&opts)))

			mgr, err := ctrl.NewManager(ctrl.GetConfigOrDie(), ctrl.Options{
				Scheme:                 scheme,
				Metrics:                metricsserver.Options{BindAddress: metricsAddr},
				HealthProbeBindAddress: probeAddr,
				LeaderElection:         enableLeaderElection,
				LeaderElectionID:       "cluster-auth-manager",
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

			addonManager, err := addonmanager.New(mgr.GetConfig())
			if err != nil {
				setupLog.Error(err, "unable to set up ready check")
				os.Exit(1)
			}

			agentAddOn, err := addonfactory.NewAgentAddonFactory(common.AddonName, manager.FS, common.AgentManifestsDir).
				WithScheme(scheme).
				WithConfigGVRs(utils.AddOnDeploymentConfigGVR).
				WithGetValuesFuncs(
					manager.GetDefaultValues(registryFQDN),
				).
				WithAgentHealthProber(agentHealthProber()).
				WithAgentInstallNamespace(func(addon *v1alpha1.ManagedClusterAddOn) (string, error) {
					return common.AddonAgentInstallNamespace, nil
				}).
				BuildHelmAgentAddon()
			if err != nil {
				setupLog.Error(err, "failed to build agent")
				os.Exit(1)
			}

			if err := addonManager.AddAgent(agentAddOn); err != nil {
				setupLog.Error(err, "unable to register addon agent")
				os.Exit(1)
			}

			if err = (&authorization.ManagedClusterRoleReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}).SetupWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "ManagedClusterRole")
				os.Exit(1)
			}
			if err = (&authorization.ManagedClusterRoleBindingReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}).SetupWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "ManagedClusterRoleBinding")
				os.Exit(1)
			}
			if err = (&authorization.ManagedClusterSetRoleBindingReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}).SetupWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "ManagedClusterSetRoleBinding")
				os.Exit(1)
			}

			if err := mgr.AddHealthzCheck("healthz", healthz.Ping); err != nil {
				setupLog.Error(err, "unable to set up health check")
				os.Exit(1)
			}
			if err := mgr.AddReadyzCheck("readyz", healthz.Ping); err != nil {
				setupLog.Error(err, "unable to set up ready check")
				os.Exit(1)
			}

			setupLog.Info("starting manager")

			ctx, cancel := context.WithCancel(ctrl.SetupSignalHandler())
			defer cancel()

			if err := addonManager.Start(ctx); err != nil {
				setupLog.Error(err, "unable to start addon agent")
				os.Exit(1)
			}

			if err := mgr.Start(ctx); err != nil {
				setupLog.Error(err, "problem running manager")
				os.Exit(1)
			}
		},
	}

	cmd.Flags().StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")
	cmd.Flags().StringVar(&registryFQDN, "registryFQDN", registryFQDN, "Docker registry FQDN used for agent image")

	fs := flag.NewFlagSet("zap", flag.ExitOnError)
	opts.BindFlags(fs)
	cmd.Flags().AddGoFlagSet(fs)

	return cmd
}
