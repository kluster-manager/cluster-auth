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
	"fmt"

	authenticationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authentication/v1alpha1"
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
	"k8s.io/client-go/rest"
	cu "kmodules.xyz/client-go/client"
	"kmodules.xyz/client-go/tools/clientcmd"
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
	sa := &core.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ace-sa-" + rand.String(7),
			Namespace: common.AddonAgentInstallNamespace,
			Labels:    managedCRB.Labels,
		},
	}

	_, err = cu.CreateOrPatch(context.Background(), r.Client, sa, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*core.ServiceAccount)
		in.ObjectMeta = sa.ObjectMeta
		return in
	})
	if err != nil {
		return err
	}

	secret, err := cu.GetServiceAccountTokenSecret(r.Client, types.NamespacedName{Name: sa.Name, Namespace: sa.Namespace})
	if err != nil {
		return err
	}

	// TODO: remove this part
	// create a ns to save the kubeconfig
	ns = &core.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: "kc-ns",
		},
	}
	_, err = cu.CreateOrPatch(context.Background(), r.Client, ns, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*core.Namespace)
		in.ObjectMeta = ns.ObjectMeta
		return in
	})
	if err != nil {
		return err
	}

	token := string(secret.Data["token"])
	restConfig, err := rest.InClusterConfig()
	if err != nil {
		return err
	}
	restConfig.BearerToken = token
	if err != nil {
		return err
	}

	usr := &authenticationv1alpha1.User{}
	if err = r.Client.Get(ctx, types.NamespacedName{Name: managedCRB.Subjects[0].Name}, usr); err != nil {
		return err
	}

	restConfig.Impersonate = rest.ImpersonationConfig{
		UserName: usr.Name,
	}
	configBytes, err := clientcmd.BuildKubeConfigBytes(restConfig, "")
	if err != nil {
		return err
	}

	sec := &core.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      managedCRB.Subjects[0].Name + "-kc",
			Namespace: ns.Name,
		},
		Data: map[string][]byte{
			"kubeconfig": configBytes,
		},
	}
	_, err = cu.CreateOrPatch(context.Background(), r.Client, sec, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*core.Secret)
		in.Data = sec.Data
		return in
	})

	if err != nil {
		return err
	}
	return nil
}

func (r *ManagedClusterRoleBindingReconciler) createClusterRoleBindingForUser(ctx context.Context, managedCRB *authorizationv1alpha1.ManagedClusterRoleBinding) error {
	userID, hubOwnerID := utils.GetUserIDAndHubOwnerIDFromLabelValues(managedCRB)

	// name: userID-hubOwnerID-gatewaybinding
	crb := &rbac.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("ace-user-%s-%s-gatewaybinding", userID, hubOwnerID),
			Labels: managedCRB.Labels,
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
	_, err := cu.CreateOrPatch(ctx, r.Client, crb, func(obj client.Object, createOp bool) client.Object {
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
	cr := &rbac.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("ace-user-%s-%s-%s", userID, hubOwnerID, rand.String(7)),
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

	_, err := cu.CreateOrPatch(ctx, r.Client, cr, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*rbac.ClusterRole)
		in.Rules = cr.Rules
		return in
	})
	if err != nil {
		return err
	}

	// now give the service-account permission to impersonate user
	// list sa
	label := managedCRB.Labels
	var saList core.ServiceAccountList
	if err = r.Client.List(ctx, &saList, client.MatchingLabelsSelector{
		Selector: labels.SelectorFromSet(label),
	}); err != nil {
		return err
	}

	sub := []rbac.Subject{
		{
			APIGroup:  "",
			Kind:      "ServiceAccount",
			Name:      saList.Items[0].Name,
			Namespace: common.AddonAgentInstallNamespace,
		},
	}
	crb := &rbac.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name:   fmt.Sprintf("ace-user-%s-%s-%s", userID, hubOwnerID, rand.String(7)),
			Labels: managedCRB.Labels,
		},
		Subjects: sub,
		RoleRef: rbac.RoleRef{
			APIGroup: rbac.GroupName,
			Kind:     "ClusterRole",
			Name:     cr.Name,
		},
	}

	_, err = cu.CreateOrPatch(context.Background(), r.Client, crb, func(obj client.Object, createOp bool) client.Object {
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
