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

	//+kubebuilder:scaffold:imports

	authenticationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authentication/v1alpha1"
	authnv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authentication/v1alpha1"
	authorizationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authorization/v1alpha1"
	authzv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authorization/v1alpha1"
	"github.com/kluster-manager/cluster-auth/pkg/addon/manager"
	"github.com/kluster-manager/cluster-auth/pkg/common"
	authorizationcontroller "github.com/kluster-manager/cluster-auth/pkg/controller/authorization"

	"github.com/spf13/cobra"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/kubernetes"
	cg "kmodules.xyz/client-go/client"
	"open-cluster-management.io/addon-framework/pkg/addonfactory"
	"open-cluster-management.io/addon-framework/pkg/agent"
	"open-cluster-management.io/addon-framework/pkg/utils"
	v1 "open-cluster-management.io/api/cluster/v1"
	mSA "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	metricsserver "sigs.k8s.io/controller-runtime/pkg/metrics/server"

	// Import all Kubernetes client auth plugins (e.g. Azure, GCP, OIDC, etc.)
	// to ensure that exec-entrypoint and run can make use of them.
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	_ "k8s.io/client-go/plugin/pkg/client/auth"
	"open-cluster-management.io/addon-framework/pkg/addonmanager"
	addonclient "open-cluster-management.io/api/client/addon/clientset/versioned"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	workv1 "open-cluster-management.io/api/work/v1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/healthz"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

var (
	scheme   = runtime.NewScheme()
	setupLog = ctrl.Log.WithName("setup")
)

func init() {
	utilruntime.Must(clientgoscheme.AddToScheme(scheme))

	utilruntime.Must(authenticationv1alpha1.AddToScheme(scheme))
	utilruntime.Must(authorizationv1alpha1.AddToScheme(scheme))
	utilruntime.Must(v1.Install(scheme))
	utilruntime.Must(workv1.Install(scheme))
	utilruntime.Must(mSA.AddToScheme(scheme))
	utilruntime.Must(rbac.AddToScheme(scheme))
}

func NewCmdManager() *cobra.Command {
	var metricsAddr string
	var enableLeaderElection bool
	var probeAddr string
	var addonAgentImageName string
	var agentInstallAll bool
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

			nativeClient, err := kubernetes.NewForConfig(mgr.GetConfig())
			if err != nil {
				setupLog.Error(err, "unable to instantiating kubernetes native client")
				os.Exit(1)
			}

			clusterClient, err := clusterclientset.NewForConfig(mgr.GetConfig())
			if err != nil {
				setupLog.Error(err, "failed to create clusterClient")
				os.Exit(1)
			}

			addonClient, err := addonclient.NewForConfig(mgr.GetConfig())
			if err != nil {
				setupLog.Error(err, "unable to instantiating ocm addon client")
				os.Exit(1)
			}

			_, err = mgr.GetRESTMapper().ResourceFor(schema.GroupVersionResource{
				Group:    authnv1alpha1.GroupVersion.Group,
				Version:  authnv1alpha1.GroupVersion.Version,
				Resource: "users",
			})
			if err != nil {
				setupLog.Error(err, `no "users" resource found in the hub cluster, is the CRD installed?`)
				os.Exit(1)
			}

			_, err = mgr.GetRESTMapper().ResourceFor(schema.GroupVersionResource{
				Group:    authnv1alpha1.GroupVersion.Group,
				Version:  authnv1alpha1.GroupVersion.Version,
				Resource: "groups",
			})
			if err != nil {
				setupLog.Error(err, `no "groups" resource found in the hub cluster, is the CRD installed?`)
				os.Exit(1)
			}

			_, err = mgr.GetRESTMapper().ResourceFor(schema.GroupVersionResource{
				Group:    authzv1alpha1.GroupVersion.Group,
				Version:  authzv1alpha1.GroupVersion.Version,
				Resource: "managedclusterroles",
			})
			if err != nil {
				setupLog.Error(err, `no "groups" resource found in the hub cluster, is the CRD installed?`)
				os.Exit(1)
			}

			_, err = mgr.GetRESTMapper().ResourceFor(schema.GroupVersionResource{
				Group:    authzv1alpha1.GroupVersion.Group,
				Version:  authzv1alpha1.GroupVersion.Version,
				Resource: "managedclusterrolebindings",
			})
			if err != nil {
				setupLog.Error(err, `no "groups" resource found in the hub cluster, is the CRD installed?`)
				os.Exit(1)
			}

			_, err = mgr.GetRESTMapper().ResourceFor(schema.GroupVersionResource{
				Group:    authzv1alpha1.GroupVersion.Group,
				Version:  authzv1alpha1.GroupVersion.Version,
				Resource: "managedclustersetrolebindings",
			})
			if err != nil {
				setupLog.Error(err, `no "groups" resource found in the hub cluster, is the CRD installed?`)
				os.Exit(1)
			}

			agentFactory := addonfactory.NewAgentAddonFactory(common.AddonName, manager.FS, "manifests/templates").
				WithConfigGVRs(utils.AddOnDeploymentConfigGVR).
				WithGetValuesFuncs(
					manager.GetDefaultValues(addonAgentImageName),
					addonfactory.GetAgentImageValues(
						addonfactory.NewAddOnDeploymentConfigGetter(addonClient),
						"Image",
						addonAgentImageName,
					),
					addonfactory.GetAddOnDeloymentConfigValues(
						addonfactory.NewAddOnDeploymentConfigGetter(addonClient),
						addonfactory.ToAddOnDeloymentConfigValues,
					),
				).
				WithAgentRegistrationOption(manager.NewRegistrationOption(nativeClient)).
				WithAgentDeployTriggerClusterFilter(utils.ClusterImageRegistriesAnnotationChanged).
				WithCreateAgentInstallNamespace()

			if agentInstallAll {
				agentFactory.WithInstallStrategy(agent.InstallAllStrategy(common.AddonAgentInstallNamespace))
			}

			agentAddOn, err := agentFactory.BuildTemplateAgentAddon()
			if err != nil {
				setupLog.Error(err, "failed to build agent")
				os.Exit(1)
			}

			if err := addonManager.AddAgent(agentAddOn); err != nil {
				setupLog.Error(err, "unable to register addon agent")
				os.Exit(1)
			}

			if err = (&authorizationcontroller.ManagedClusterRoleReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}).SetupWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "ManagedClusterRole")
				os.Exit(1)
			}
			if err = (&authorizationcontroller.ManagedClusterRoleBindingReconciler{
				Client: mgr.GetClient(),
				Scheme: mgr.GetScheme(),
			}).SetupWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "ManagedClusterRoleBinding")
				os.Exit(1)
			}
			if err = (&authorizationcontroller.ManagedClusterSetRoleBindingReconciler{
				Client:        mgr.GetClient(),
				Scheme:        mgr.GetScheme(),
				ClusterClient: *clusterClient,
			}).SetupWithManager(mgr); err != nil {
				setupLog.Error(err, "unable to create controller", "controller", "ManagedClusterSetRoleBinding")
				os.Exit(1)
			}
			//+kubebuilder:scaffold:builder

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

			// create clusterRole with "clusterGateway/proxy" rules
			gatewayClusterRole := &rbac.ClusterRole{
				ObjectMeta: metav1.ObjectMeta{
					Name: "cluster-gateway-permission",
				},
				Rules: []rbac.PolicyRule{
					{
						APIGroups: []string{"cluster.core.oam.dev"},
						Resources: []string{"clustergateways/health", "clustergateways/proxy"},
						Verbs:     []string{"*"},
					},
				},
			}

			_, err = cg.CreateOrPatch(context.Background(), mgr.GetClient(), gatewayClusterRole, func(obj client.Object, createOp bool) client.Object {
				in := obj.(*rbac.ClusterRole)
				in.Rules = gatewayClusterRole.Rules
				return in
			})
			if err != nil {
				setupLog.Error(err, "problem creating cluster-gateway-permission")
				os.Exit(1)
			}
		},
	}

	cmd.Flags().StringVar(&metricsAddr, "metrics-bind-address", ":8080", "The address the metric endpoint binds to.")
	cmd.Flags().StringVar(&probeAddr, "health-probe-bind-address", ":8081", "The address the probe endpoint binds to.")
	cmd.Flags().BoolVar(&enableLeaderElection, "leader-elect", false,
		"Enable leader election for controller manager. "+
			"Enabling this will ensure there is only one active controller manager.")

	cmd.Flags().StringVar(&addonAgentImageName, "agent-image-name", "docker.io/rokibulhasan114/cluster-auth:latest",
		"The image name of the addon agent")
	cmd.Flags().BoolVar(
		&agentInstallAll, "agent-install-all", true,
		"Configure the install strategy of agent on managed clusters. "+
			"Enabling this will automatically install agent on all managed cluster.")

	fs := flag.NewFlagSet("zap", flag.ExitOnError)
	opts.BindFlags(fs)
	cmd.Flags().AddGoFlagSet(fs)

	return cmd
}
