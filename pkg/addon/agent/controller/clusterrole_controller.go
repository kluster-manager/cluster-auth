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

package controller

import (
	"context"
	authorizationv1alpha1 "github.com/kluster-manager/cluster-auth/api/authorization/v1alpha1"
	"github.com/kluster-manager/cluster-auth/pkg/util"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/labels"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"open-cluster-management.io/managed-serviceaccount/apis/authentication/v1alpha1"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	cg "kmodules.xyz/client-go/client"
	ocmkl "open-cluster-management.io/api/operator/v1"
)

const ManagedServiceAccountNamespace = "open-cluster-management-managed-serviceaccount"

// ManagedClusterRoleBindingReconciler reconciles a ManagedClusterRoleBinding object
type ClusterRoleReconciler struct {
	HubClient         client.Client
	HubNativeClient   kubernetes.Interface
	SpokeNativeClient kubernetes.Interface
	SpokeClientConfig *rest.Config
	SpokeNamespace    string
	ClusterName       string
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
func (r *ClusterRoleReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Start reconciling")

	c, err := util.GetKubeClient(r.SpokeClientConfig)
	if err != nil {
		return reconcile.Result{}, nil
	}

	cr := &rbacv1.ClusterRole{}
	if err = c.Get(ctx, req.NamespacedName, cr); err != nil {
		return reconcile.Result{}, err
	}

	msaName, ok := cr.Labels["authentication.k8s.appscode.com/managed-serviceaccount"]
	if !ok {
		return reconcile.Result{}, err
	}

	// get klusterlet
	kl := ocmkl.Klusterlet{}
	err = c.Get(context.Background(), client.ObjectKey{Name: "klusterlet"}, &kl)
	if err != nil {
		return reconcile.Result{}, err
	}

	// get serviceaccount
	managedSA := &v1alpha1.ManagedServiceAccount{}
	if err = r.HubClient.Get(context.Background(), types.NamespacedName{Name: msaName, Namespace: kl.Spec.ClusterName}, managedSA); err != nil {
		return reconcile.Result{}, err
	}

	usrName, ok := cr.Labels["authentication.k8s.appscode.com/username"]
	if !ok {
		return reconcile.Result{}, err
	}

	userSub := []rbacv1.Subject{
		{
			APIGroup: "",
			Kind:     "User",
			Name:     usrName,
		},
	}

	managedCRBList := &authorizationv1alpha1.ManagedClusterRoleBindingList{}
	err = r.HubClient.List(ctx, managedCRBList, &client.ListOptions{
		Namespace: kl.Spec.ClusterName,
		LabelSelector: labels.SelectorFromSet(map[string]string{
			"authentication.k8s.appscode.com/username": usrName,
		}),
	})
	if err != nil {
		return reconcile.Result{}, err
	}

	for _, managedCRB := range managedCRBList.Items {
		// this clusterRoleBinding will give permission to the user
		crb := &rbacv1.ClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name: managedCRB.Name + "-" + managedCRB.RoleRef.Name,
			},
			Subjects: userSub, // user reference
			RoleRef: rbacv1.RoleRef{
				APIGroup: managedCRB.RoleRef.APIGroup,
				Kind:     managedCRB.RoleRef.Kind,
				Name:     managedCRB.RoleRef.Name,
			},
		}

		_, err = cg.CreateOrPatch(context.Background(), c, crb, func(obj client.Object, createOp bool) client.Object {
			in := obj.(*rbacv1.ClusterRoleBinding)
			in.Subjects = crb.Subjects
			in.RoleRef = crb.RoleRef
			return in
		})
		if err != nil {
			return reconcile.Result{}, err
		}
	}
	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClusterRoleReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&rbacv1.ClusterRole{}).Watches(&rbacv1.ClusterRole{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
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
