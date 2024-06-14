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
	"errors"
	"fmt"

	authorizationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authorization/v1alpha1"
	"github.com/kluster-manager/cluster-auth/pkg/common"
	"github.com/kluster-manager/cluster-auth/pkg/utils"

	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/rand"
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

	// create a service account for user
	if err = r.createServiceAccountForUser(ctx, managedCRB); err != nil {
		return reconcile.Result{}, err
	}

	// give user the gateway permission
	if err = r.createClusterRoleBindingForUser(ctx, managedCRB); err != nil {
		return reconcile.Result{}, err
	}

	if err = r.createClusterRoleAndClusterRoleBindingToImpersonate(ctx, managedCRB); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *ManagedClusterRoleBindingReconciler) createServiceAccountForUser(ctx context.Context, managedCRB *authorizationv1alpha1.ManagedClusterRoleBinding) error {
	// create ns to store service-accounts and tokens of users
	ns := &core.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: common.AddonAgentInstallNamespace,
		},
	}

	_, err := cu.CreateOrPatch(context.Background(), r.Client, ns, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*core.Namespace)
		in.ObjectMeta = ns.ObjectMeta
		return in
	})
	if err != nil {
		return err
	}

	// create service-account for user
	var saList core.ServiceAccountList
	_ = r.Client.List(ctx, &saList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(managedCRB.Labels),
	})

	sa := core.ServiceAccount{}
	if len(saList.Items) == 0 {
		sa.Name = "ace-sa-" + rand.String(7)
		sa.Namespace = common.AddonAgentInstallNamespace
		sa.Labels = managedCRB.Labels
	} else {
		sa = saList.Items[0]
	}

	_, err = cu.CreateOrPatch(context.Background(), r.Client, &sa, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*core.ServiceAccount)
		in.ObjectMeta = sa.ObjectMeta
		return in
	})
	if err != nil {
		return err
	}

	_, err = cu.GetServiceAccountTokenSecret(r.Client, types.NamespacedName{Name: sa.Name, Namespace: sa.Namespace})
	if err != nil {
		return err
	}

	return nil
}

func (r *ManagedClusterRoleBindingReconciler) createClusterRoleBindingForUser(ctx context.Context, managedCRB *authorizationv1alpha1.ManagedClusterRoleBinding) error {
	userID, hubOwnerID := utils.GetUserIDAndHubOwnerIDFromLabelValues(managedCRB)

	// name: userID-hubOwnerID-gatewaybinding
	crb := rbac.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s-%s-gatewaybinding", userID, hubOwnerID),
			Labels: map[string]string{
				common.UserAuthLabel: userID, // TODO: remove this cluster-rolebinding when user object is deleted from hub
			},
		},
		Subjects: []rbac.Subject{
			{
				APIGroup: "",
				Kind:     "User",
				Name:     managedCRB.Subjects[0].Name,
			},
		},
		RoleRef: rbac.RoleRef{
			APIGroup: rbac.GroupName,
			Kind:     "ClusterRole",
			Name:     common.GatewayProxyClusterRole,
		},
	}

	crbList := &rbac.ClusterRoleBindingList{}
	_ = r.Client.List(ctx, crbList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(managedCRB.Labels),
	})

	if len(crbList.Items) > 0 {
		crb = crbList.Items[0]
	}
	_, err := cu.CreateOrPatch(ctx, r.Client, &crb, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*rbac.ClusterRoleBinding)
		in.Subjects = crb.Subjects
		in.RoleRef = crb.RoleRef
		return in
	})
	if err != nil {
		return err
	}
	return nil
}

func (r *ManagedClusterRoleBindingReconciler) createClusterRoleAndClusterRoleBindingToImpersonate(ctx context.Context, managedCRB *authorizationv1alpha1.ManagedClusterRoleBinding) error {
	userID, hubOwnerID := utils.GetUserIDAndHubOwnerIDFromLabelValues(managedCRB)
	userName := managedCRB.Subjects[0].Name
	// name: userID-hubOwnerID-randomString
	// impersonate clusterRole
	cr := rbac.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("%s-%s-%s", userID, hubOwnerID, rand.String(7)),
			Labels: managedCRB.Labels,
		},
		Rules: []rbac.PolicyRule{
			{
				APIGroups:     []string{""},
				Resources:     []string{"users"},
				Verbs:         []string{"impersonate"},
				ResourceNames: []string{userName},
			},
		},
	}

	crList := &rbac.ClusterRoleList{}
	_ = r.Client.List(ctx, crList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(managedCRB.Labels),
	})

	if len(crList.Items) > 0 {
		cr = crList.Items[0]
	}
	_, err := cu.CreateOrPatch(ctx, r.Client, &cr, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*rbac.ClusterRole)
		in.ObjectMeta = cr.ObjectMeta
		in.Rules = cr.Rules
		return in
	})
	if err != nil {
		return err
	}

	// now give the service-account permission to impersonate user
	var saList core.ServiceAccountList
	_ = r.Client.List(ctx, &saList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(managedCRB.Labels),
	})

	if len(saList.Items) == 0 {
		return errors.New("service account not found")
	}
	sub := []rbac.Subject{
		{
			APIGroup:  "",
			Kind:      "ServiceAccount",
			Name:      saList.Items[0].Name,
			Namespace: common.AddonAgentInstallNamespace,
		},
	}

	crb := rbac.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   cr.Name, // creating cluster-rolebinding name with the same name of cluster-role
			Labels: managedCRB.Labels,
		},
		Subjects: sub,
		RoleRef: rbac.RoleRef{
			APIGroup: rbac.GroupName,
			Kind:     "ClusterRole",
			Name:     cr.Name,
		},
	}

	crbList := &rbac.ClusterRoleBindingList{}
	_ = r.Client.List(ctx, crbList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(managedCRB.Labels),
	})

	if len(crbList.Items) > 0 {
		crb = crbList.Items[0]
	}

	_, err = cu.CreateOrPatch(context.Background(), r.Client, &crb, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*rbac.ClusterRoleBinding)
		in.ObjectMeta = crb.ObjectMeta
		in.Subjects = crb.Subjects
		in.RoleRef = crb.RoleRef
		return in
	})
	if err != nil {
		return err
	}

	return nil
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
