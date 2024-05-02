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

	authenticationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authentication/v1alpha1"
	authorizationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authorization/v1alpha1"
	"github.com/kluster-manager/cluster-auth/pkg/common"

	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	cu "kmodules.xyz/client-go/client"
	"kmodules.xyz/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	// create a service account for user
	if err = createServiceAccountForUser(ctx, r.Client, managedCRB); err != nil {
		return reconcile.Result{}, err
	}

	// give user the gateway permission
	if err = createClusterRoleBindingForUser(ctx, r.Client, managedCRB); err != nil {
		return reconcile.Result{}, err
	}

	if err = createClusterRoleAndClusterRoleBindingToImpersonate(ctx, r.Client, managedCRB); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func createClusterRoleBindingForUser(ctx context.Context, c client.Client, managedCRB *authorizationv1alpha1.ManagedClusterRoleBinding) error {
	crb := &rbac.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: managedCRB.Subjects[0].Name + "-gatewaybinding",
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
	_, err := cu.CreateOrPatch(ctx, c, crb, func(obj client.Object, createOp bool) client.Object {
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

func createServiceAccountForUser(ctx context.Context, c client.Client, managedCRB *authorizationv1alpha1.ManagedClusterRoleBinding) error {
	// create ns to store service-accounts and tokens of users
	ns := &core.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: common.AddonAgentInstallNamespace,
		},
	}

	_, err := cu.CreateOrPatch(context.Background(), c, ns, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*core.Namespace)
		in.ObjectMeta = ns.ObjectMeta
		return in
	})
	if err != nil {
		return err
	}

	// create sa for user
	sa := &core.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ace-sa-" + managedCRB.Subjects[0].Name,
			Namespace: common.AddonAgentInstallNamespace,
		},
	}
	_, err = cu.CreateOrPatch(context.Background(), c, sa, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*core.ServiceAccount)
		in.ObjectMeta = sa.ObjectMeta
		return in
	})
	if err != nil {
		return err
	}

	secret, err := cu.GetServiceAccountTokenSecret(c, types.NamespacedName{Name: sa.Name, Namespace: sa.Namespace})
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
	_, err = cu.CreateOrPatch(context.Background(), c, ns, func(obj client.Object, createOp bool) client.Object {
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
	if err = c.Get(ctx, types.NamespacedName{Name: managedCRB.Subjects[0].Name}, usr); err != nil {
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
	_, err = cu.CreateOrPatch(context.Background(), c, sec, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*core.Secret)
		in.Data = sec.Data
		return in
	})

	if err != nil {
		return err
	}
	return nil
}

func createClusterRoleAndClusterRoleBindingToImpersonate(ctx context.Context, c client.Client, managedCRB *authorizationv1alpha1.ManagedClusterRoleBinding) error {
	userName := managedCRB.Subjects[0].Name
	// impersonate clusterRole
	cr := &rbac.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ace-user-" + userName,
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

	_, err := cu.CreateOrPatch(ctx, c, cr, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*rbac.ClusterRole)
		in.Rules = cr.Rules
		return in
	})
	if err != nil {
		return err
	}

	// now give the service-account permission to impersonate user
	sub := []rbac.Subject{
		{
			APIGroup:  "",
			Kind:      "ServiceAccount",
			Name:      "ace-sa-" + userName,
			Namespace: common.AddonAgentInstallNamespace,
		},
	}
	crb := &rbac.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: "ace-user-" + userName,
		},
		Subjects: sub,
		RoleRef: rbac.RoleRef{
			APIGroup: rbac.GroupName,
			Kind:     "ClusterRole",
			Name:     cr.Name,
		},
	}

	_, err = cu.CreateOrPatch(context.Background(), c, crb, func(obj client.Object, createOp bool) client.Object {
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

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedClusterRoleBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&authorizationv1alpha1.ManagedClusterRoleBinding{}).Watches(&authorizationv1alpha1.ManagedClusterRoleBinding{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
