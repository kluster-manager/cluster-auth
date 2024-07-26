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

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	cu "kmodules.xyz/client-go/client"
	clusterv1 "open-cluster-management.io/api/cluster/v1"
	clusterv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	clustersdkv1beta2 "open-cluster-management.io/sdk-go/pkg/apis/cluster/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ManagedClusterSetRoleBindingReconciler reconciles a ManagedClusterSetRoleBinding object
type ManagedClusterSetRoleBindingReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ManagedClusterSetRoleBinding object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *ManagedClusterSetRoleBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Start reconciling")

	managedCSRB := &authorizationv1alpha1.ManagedClusterSetRoleBinding{}
	err := r.Client.Get(ctx, req.NamespacedName, managedCSRB)
	if err != nil {
		return reconcile.Result{}, err
	}

	// Check if the managedCRB is marked for deletion
	if managedCSRB.GetDeletionTimestamp() != nil {
		if controllerutil.ContainsFinalizer(managedCSRB, common.HubAuthorizationFinalizer) {
			// Perform cleanup logic, e.g., delete related resources
			if err := r.deleteAssociatedResources(managedCSRB); err != nil {
				return reconcile.Result{}, err
			}
			// Remove the finalizer
			controllerutil.RemoveFinalizer(managedCSRB, common.HubAuthorizationFinalizer)
			if err := r.Client.Update(context.TODO(), managedCSRB); err != nil {
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	// Add finalizer if not present
	if err := r.addFinalizerIfNeeded(managedCSRB); err != nil {
		return reconcile.Result{}, err
	}

	var managedClusterSet clusterv1beta2.ManagedClusterSet
	err = r.Get(ctx, types.NamespacedName{Name: managedCSRB.ClusterSetRef.Name}, &managedClusterSet)
	if err != nil {
		return reconcile.Result{}, err
	}

	sel, err := clustersdkv1beta2.BuildClusterSelector(&managedClusterSet)
	if err != nil {
		return reconcile.Result{}, err
	}
	var clusters clusterv1.ManagedClusterList
	err = r.List(ctx, &clusters, client.MatchingLabelsSelector{Selector: sel})
	if err != nil {
		return reconcile.Result{}, err
	}

	// create managedClusterRoleBinding for every cluster of this clusterSet
	for _, cluster := range clusters.Items {
		managedCRB := &authorizationv1alpha1.ManagedClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      managedCSRB.Name + "-" + cluster.Name,
				Namespace: cluster.Name,
				Labels:    managedCSRB.Labels,
			},
			Subjects: managedCSRB.Subjects,
			RoleRef:  authorizationv1alpha1.ClusterRoleRef(managedCSRB.RoleRef),
		}

		_, err = cu.CreateOrPatch(context.Background(), r.Client, managedCRB, func(obj client.Object, createOp bool) client.Object {
			in := obj.(*authorizationv1alpha1.ManagedClusterRoleBinding)
			in.Subjects = managedCRB.Subjects
			in.RoleRef = managedCRB.RoleRef
			return in
		})
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// AddFinalizerIfNeeded adds a finalizer to the CRD instance if it doesn't already have one
func (r *ManagedClusterSetRoleBindingReconciler) addFinalizerIfNeeded(managedCSRB *authorizationv1alpha1.ManagedClusterSetRoleBinding) error {
	if !controllerutil.ContainsFinalizer(managedCSRB, common.HubAuthorizationFinalizer) {
		controllerutil.AddFinalizer(managedCSRB, common.HubAuthorizationFinalizer)
		if err := r.Client.Update(context.TODO(), managedCSRB); err != nil {
			return err
		}
	}
	return nil
}

func (r *ManagedClusterSetRoleBindingReconciler) deleteAssociatedResources(managedCSRB *authorizationv1alpha1.ManagedClusterSetRoleBinding) error {
	managedClusterRoleBindingList := authorizationv1alpha1.ManagedClusterRoleBindingList{}
	err := r.Client.List(context.TODO(), &managedClusterRoleBindingList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(managedCSRB.Labels),
	})
	if err == nil {
		for _, mcrb := range managedClusterRoleBindingList.Items {
			if err := r.Client.Delete(context.TODO(), &mcrb); err != nil {
				return err
			}
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedClusterSetRoleBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&authorizationv1alpha1.ManagedClusterSetRoleBinding{}).
		Complete(r)
}
