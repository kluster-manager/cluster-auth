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

package authorization

import (
	"context"

	authorizationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authorization/v1alpha1"
	"github.com/kluster-manager/cluster-auth/pkg/common"

	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ManagedClusterRoleBindingReconciler reconciles a ManagedClusterRoleBinding object
type ManagedClusterRoleBindingReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ManagedClusterRoleBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Start reconciling")

	managedCRB := &authorizationv1alpha1.ManagedClusterRoleBinding{}
	err := r.Client.Get(ctx, req.NamespacedName, managedCRB)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Check if the managedCRB is marked for deletion
	if managedCRB.GetDeletionTimestamp() != nil {
		if controllerutil.ContainsFinalizer(managedCRB, common.HubAuthorizationFinalizer) {
			// Perform cleanup logic, e.g., delete related resources
			if err := r.deleteAssociatedResources(managedCRB); err != nil {
				return reconcile.Result{}, err
			}
			// Remove the finalizer
			controllerutil.RemoveFinalizer(managedCRB, common.HubAuthorizationFinalizer)
			if err := r.Client.Update(context.TODO(), managedCRB); err != nil {
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	// Add finalizer if not present
	if err := r.addFinalizerIfNeeded(managedCRB); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

// AddFinalizerIfNeeded adds a finalizer to the CRD instance if it doesn't already have one
func (r *ManagedClusterRoleBindingReconciler) addFinalizerIfNeeded(managedCRB *authorizationv1alpha1.ManagedClusterRoleBinding) error {
	if !controllerutil.ContainsFinalizer(managedCRB, common.HubAuthorizationFinalizer) {
		controllerutil.AddFinalizer(managedCRB, common.HubAuthorizationFinalizer)
		if err := r.Client.Update(context.TODO(), managedCRB); err != nil {
			return err
		}
	}
	return nil
}

func (r *ManagedClusterRoleBindingReconciler) deleteAssociatedResources(managedCRB *authorizationv1alpha1.ManagedClusterRoleBinding) error {
	saList := core.ServiceAccountList{}
	err := r.Client.List(context.TODO(), &saList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(managedCRB.Labels),
	})
	if err == nil {
		for _, sa := range saList.Items {
			if err := r.Client.Delete(context.TODO(), &sa); err != nil {
				return err
			}
		}
	}

	crList := rbac.ClusterRoleList{}
	err = r.Client.List(context.TODO(), &crList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(managedCRB.Labels),
	})
	if err == nil {
		for _, cr := range crList.Items {
			if err := r.Client.Delete(context.TODO(), &cr); err != nil {
				return err
			}
		}
	}

	crbList := rbac.ClusterRoleBindingList{}
	err = r.Client.List(context.TODO(), &crbList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(managedCRB.Labels),
	})
	if err == nil {
		for _, crb := range crbList.Items {
			if err := r.Client.Delete(context.TODO(), &crb); err != nil {
				return err
			}
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedClusterRoleBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&authorizationv1alpha1.ManagedClusterRoleBinding{}).Watches(&authorizationv1alpha1.ManagedClusterRoleBinding{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
