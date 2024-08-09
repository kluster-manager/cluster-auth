package authentication

import (
	"context"
	"fmt"

	authenticationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authentication/v1alpha1"
	"github.com/kluster-manager/cluster-auth/pkg/common"
	"github.com/kluster-manager/cluster-auth/pkg/utils"

	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	cu "kmodules.xyz/client-go/client"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
)

type AccountReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *AccountReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Start reconciling...")

	usr := &authenticationv1alpha1.Account{}
	err := r.Client.Get(ctx, req.NamespacedName, usr)
	if err != nil {
		return reconcile.Result{}, err
	}

	if err = r.createServiceAccount(ctx, usr); err != nil {
		return reconcile.Result{}, err
	}

	if err = r.createGatewayClusterRoleBindingForUser(ctx, usr); err != nil {
		return reconcile.Result{}, err
	}

	if err = r.createClusterRoleAndClusterRoleBindingToImpersonate(ctx, usr); err != nil {
		return reconcile.Result{}, err
	}

	return reconcile.Result{}, nil
}

func (r *AccountReconciler) createServiceAccount(ctx context.Context, acc *authenticationv1alpha1.Account) error {
	// create ns to store service-accounts and tokens of accounts
	ns := core.Namespace{
		ObjectMeta: metav1.ObjectMeta{
			Name: common.AddonAgentInstallNamespace,
		},
	}

	_, err := cu.CreateOrPatch(ctx, r.Client, &ns, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*core.Namespace)
		in.ObjectMeta = ns.ObjectMeta
		return in
	})
	if err != nil {
		return err
	}

	// create service-account for user
	sa := core.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      utils.ReplaceColonWithHyphen(acc.Name),
			Namespace: common.AddonAgentInstallNamespace,
		},
	}

	_, err = cu.CreateOrPatch(ctx, r.Client, &sa, func(obj client.Object, createOp bool) client.Object {
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

func (r *AccountReconciler) createGatewayClusterRoleBindingForUser(ctx context.Context, acc *authenticationv1alpha1.Account) error {
	sub := []rbac.Subject{
		{
			APIGroup: "",
			Kind:     "User",
			Name:     acc.Name,
		},
	}

	if acc.Spec.Type == authenticationv1alpha1.AccountTypeServiceAccount {
		sub = []rbac.Subject{
			{
				APIGroup:  "",
				Kind:      "ServiceAccount",
				Name:      utils.ReplaceColonWithHyphen(acc.Name),
				Namespace: common.AddonAgentInstallNamespace,
			},
		}
	}

	crb := rbac.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("ace:%s:proxy", acc.Spec.UID),
		},
		Subjects: sub,
		RoleRef: rbac.RoleRef{
			APIGroup: rbac.GroupName,
			Kind:     "ClusterRole",
			Name:     common.GatewayProxyClusterRole,
		},
	}

	if acc.Spec.Type == authenticationv1alpha1.AccountTypeServiceAccount {
		crb.Name = fmt.Sprintf("ace:%s:proxy", acc.Spec.Username)
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

func (r *AccountReconciler) createClusterRoleAndClusterRoleBindingToImpersonate(ctx context.Context, acc *authenticationv1alpha1.Account) error {
	// impersonate clusterRole
	cr := rbac.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("ace:%s:impersonate", acc.Spec.UID),
		},
		Rules: []rbac.PolicyRule{
			{
				APIGroups:     []string{""},
				Resources:     []string{"users"},
				Verbs:         []string{"impersonate"},
				ResourceNames: []string{acc.Name},
			},
		},
	}

	if acc.Spec.Type == authenticationv1alpha1.AccountTypeServiceAccount {
		cr = rbac.ClusterRole{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("ace:%s:impersonate", acc.Spec.Username),
			},
			Rules: []rbac.PolicyRule{
				{
					APIGroups:     []string{""},
					Resources:     []string{"serviceaccounts"},
					Verbs:         []string{"impersonate"},
					ResourceNames: []string{utils.ReplaceColonWithHyphen(acc.Name)},
				},
			},
		}
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

	sub := []rbac.Subject{
		{
			APIGroup:  "",
			Kind:      "ServiceAccount",
			Name:      utils.ReplaceColonWithHyphen(acc.Name),
			Namespace: common.AddonAgentInstallNamespace,
		},
	}

	crb := rbac.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name, // creating cluster-rolebinding name with the same name of cluster-role
		},
		Subjects: sub,
		RoleRef: rbac.RoleRef{
			APIGroup: rbac.GroupName,
			Kind:     "ClusterRole",
			Name:     cr.Name,
		},
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

// SetupWithManager sets up the controller with the Manager.
func (r *AccountReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&authenticationv1alpha1.Account{}).Watches(&authenticationv1alpha1.Account{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
