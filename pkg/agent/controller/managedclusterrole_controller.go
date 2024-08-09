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

package controller

import (
	"context"

	authorizationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authorization/v1alpha1"

	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cu "kmodules.xyz/client-go/client"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ManagedClusterRoleReconciler reconciles a ManagedClusterRole object
type ManagedClusterRoleReconciler struct {
	HubClient   client.Client
	SpokeClient client.Client
	ClusterName string
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ManagedClusterRole object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *ManagedClusterRoleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Start reconciling")

	managedClusterRole := &authorizationv1alpha1.ManagedClusterRole{}
	if err := r.HubClient.Get(ctx, req.NamespacedName, managedClusterRole); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// create clusterRole in spoke cluster
	cr := &rbac.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   managedClusterRole.Name,
			Labels: managedClusterRole.Labels,
		},
		Rules: managedClusterRole.Rules,
	}

	_, err := cu.CreateOrPatch(ctx, r.SpokeClient, cr, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*rbac.ClusterRole)
		in.Rules = cr.Rules
		return in
	})
	if err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedClusterRoleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&authorizationv1alpha1.ManagedClusterRole{}).Watches(&authorizationv1alpha1.ManagedClusterRole{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
