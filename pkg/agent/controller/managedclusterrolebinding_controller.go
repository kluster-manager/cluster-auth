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

	authzv1alpah1 "github.com/kluster-manager/cluster-auth/apis/authorization/v1alpha1"
	"github.com/kluster-manager/cluster-auth/pkg/common"
	"github.com/kluster-manager/cluster-auth/pkg/utils"

	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	cu "kmodules.xyz/client-go/client"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

// ManagedClusterRoleBindingReconciler reconciles a ManagedClusterRoleBinding object
type ManagedClusterRoleBindingReconciler struct {
	HubClient   client.Client
	SpokeClient client.Client
}

//+kubebuilder:rbac:groups=authorization.k8s.appscode.com,resources=managedclusterrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=authorization.k8s.appscode.com,resources=managedclusterrolebindings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=authorization.k8s.appscode.com,resources=managedclusterrolebindings/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ManagedClusterRoleBinding object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *ManagedClusterRoleBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Start reconciling...")

	var managedCRB authzv1alpah1.ManagedClusterRoleBinding
	if err := r.HubClient.Get(ctx, req.NamespacedName, &managedCRB); err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Check if the managedCRB is marked for deletion
	if managedCRB.GetDeletionTimestamp() != nil {
		if controllerutil.ContainsFinalizer(&managedCRB, common.SpokeAuthorizationFinalizer) {
			// Perform cleanup logic, e.g., delete related resources
			if err := r.deleteAssociatedResources(&managedCRB); err != nil {
				return reconcile.Result{}, err
			}
			// Remove the finalizer
			controllerutil.RemoveFinalizer(&managedCRB, common.SpokeAuthorizationFinalizer)
			if err := r.HubClient.Update(context.TODO(), &managedCRB); err != nil {
				return reconcile.Result{}, err
			}
		}
		return reconcile.Result{}, nil
	}

	// Add finalizer if not present
	if err := r.addFinalizerIfNeeded(&managedCRB); err != nil {
		return reconcile.Result{}, err
	}

	// now give actual permission to the User
	sub := getSubject(managedCRB)

	if len(managedCRB.RoleRef.Namespaces) == 0 {
		givenClusterRolebinding := &rbac.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: rbac.SchemeGroupVersion.String(),
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name:   managedCRB.Name,
				Labels: managedCRB.Labels,
			},
			Subjects: sub,
			RoleRef: rbac.RoleRef{
				APIGroup: managedCRB.RoleRef.APIGroup,
				Kind:     managedCRB.RoleRef.Kind,
				Name:     managedCRB.RoleRef.Name,
			},
		}
		_, err := cu.CreateOrPatch(context.Background(), r.SpokeClient, givenClusterRolebinding, func(obj client.Object, createOp bool) client.Object {
			in := obj.(*rbac.ClusterRoleBinding)
			in.Subjects = givenClusterRolebinding.Subjects
			in.RoleRef = givenClusterRolebinding.RoleRef
			return in
		})
		if err != nil {
			return reconcile.Result{}, err
		}
	} else {
		for _, ns := range managedCRB.RoleRef.Namespaces {
			_, err := utils.IsNamespaceExist(r.SpokeClient, ns)
			if err != nil {
				return reconcile.Result{}, err
			}

			givenRolebinding := &rbac.RoleBinding{
				TypeMeta: metav1.TypeMeta{
					APIVersion: rbac.SchemeGroupVersion.String(),
					Kind:       "RoleBinding",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      managedCRB.Name,
					Namespace: ns,
					Labels:    managedCRB.Labels,
				},
				Subjects: sub,
				RoleRef: rbac.RoleRef{
					APIGroup: managedCRB.RoleRef.APIGroup,
					Kind:     managedCRB.RoleRef.Kind,
					Name:     managedCRB.RoleRef.Name,
				},
			}

			_, err = cu.CreateOrPatch(context.Background(), r.SpokeClient, givenRolebinding, func(obj client.Object, createOp bool) client.Object {
				in := obj.(*rbac.RoleBinding)
				in.Subjects = givenRolebinding.Subjects
				in.RoleRef = givenRolebinding.RoleRef
				return in
			})
			if err != nil {
				return reconcile.Result{}, err
			}
		}
	}
	return reconcile.Result{}, nil
}

// AddFinalizerIfNeeded adds a finalizer to the CRD instance if it doesn't already have one
func (r *ManagedClusterRoleBindingReconciler) addFinalizerIfNeeded(managedCRB *authzv1alpah1.ManagedClusterRoleBinding) error {
	if !controllerutil.ContainsFinalizer(managedCRB, common.SpokeAuthorizationFinalizer) {
		controllerutil.AddFinalizer(managedCRB, common.SpokeAuthorizationFinalizer)
		if err := r.HubClient.Update(context.TODO(), managedCRB); err != nil {
			return err
		}
	}
	return nil
}

func (r *ManagedClusterRoleBindingReconciler) deleteAssociatedResources(managedCRB *authzv1alpah1.ManagedClusterRoleBinding) error {
	crList := rbac.ClusterRoleList{}
	err := r.SpokeClient.List(context.TODO(), &crList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(managedCRB.Labels),
	})
	if err == nil {
		for _, cr := range crList.Items {
			if err := r.SpokeClient.Delete(context.TODO(), &cr); err != nil {
				return err
			}
		}
	}

	crbList := rbac.ClusterRoleBindingList{}
	err = r.SpokeClient.List(context.TODO(), &crbList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(managedCRB.Labels),
	})
	if err == nil {
		for _, crb := range crbList.Items {
			if err := r.SpokeClient.Delete(context.TODO(), &crb); err != nil {
				return err
			}
		}
	}

	rbList := rbac.RoleBindingList{}
	err = r.SpokeClient.List(context.TODO(), &rbList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(managedCRB.Labels),
	})
	if err == nil {
		for _, rb := range rbList.Items {
			if err := r.SpokeClient.Delete(context.TODO(), &rb); err != nil {
				return err
			}
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedClusterRoleBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&authzv1alpah1.ManagedClusterRoleBinding{}).Watches(&authzv1alpah1.ManagedClusterRoleBinding{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}

func getSubject(managedCRB authzv1alpah1.ManagedClusterRoleBinding) []rbac.Subject {
	subs := make([]rbac.Subject, 0, len(managedCRB.Subjects))
	for _, sub := range managedCRB.Subjects {
		subs = append(subs, rbac.Subject{
			APIGroup: sub.APIGroup,
			Kind:     sub.Kind,
			Name:     sub.Name,
		})
	}
	return subs
}

/*
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRole
metadata:
  name: cluster-admin-impersonator
rules:
- apiGroups: [""]
  resources: ["serviceaccounts"]
  verbs: ["impersonate"]
  resourceNames: ["cluster-admin"]
---
apiVersion: rbac.authorization.k8s.io/v1
kind: ClusterRoleBinding
metadata:
  name: cluster-admin-impersonate
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: ClusterRole
  name: cluster-admin-impersonator
subjects:
- apiGroup: rbac.authorization.k8s.io
  kind: Group
  name: ops-team
*/
