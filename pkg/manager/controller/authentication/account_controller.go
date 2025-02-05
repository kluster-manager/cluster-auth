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

package authentication

import (
	"context"
	"fmt"
	"strings"

	authenticationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authentication/v1alpha1"
	"github.com/kluster-manager/cluster-auth/pkg/common"
	"github.com/kluster-manager/cluster-auth/pkg/utils"

	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	kmapi "kmodules.xyz/client-go/api/v1"
	cu "kmodules.xyz/client-go/client"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
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

	acc := &authenticationv1alpha1.Account{}
	err := r.Client.Get(ctx, req.NamespacedName, acc)
	if err != nil {
		return reconcile.Result{}, client.IgnoreNotFound(err)
	}

	// Update status to InProgress only if not already set
	if acc.Status.Phase == "" {
		acc.Status.Phase = authenticationv1alpha1.AccountPhaseInProgress
		setAccountType(acc)
		if err := r.Client.Status().Update(ctx, acc); err != nil {
			return reconcile.Result{}, err
		}
	}
	if err = r.createServiceAccount(ctx, acc); err != nil {
		return reconcile.Result{}, r.setStatusFailed(ctx, acc, err)
	}

	if err = r.createGatewayClusterRoleBindingForUser(ctx, acc); err != nil {
		return reconcile.Result{}, r.setStatusFailed(ctx, acc, err)
	}

	if err = r.createImpersonateClusterRoleAndRoleBinding(ctx, acc); err != nil {
		return reconcile.Result{}, r.setStatusFailed(ctx, acc, err)
	}

	// Set the status to success after successful reconciliation
	if acc.Status.Phase != authenticationv1alpha1.AccountPhaseCurrent {
		if err := r.setStatusSuccess(ctx, acc, "Reconciliation completed successfully."); err != nil {
			return reconcile.Result{}, err
		}
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
			Name:      acc.Name,
			Namespace: common.AddonAgentInstallNamespace,
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(acc, authenticationv1alpha1.GroupVersion.WithKind("Account")),
			},
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
	// Determine subject kind based on whether the username contains the service account prefix
	subKind := "User"
	if strings.Contains(acc.Spec.Username, common.ServiceAccountPrefix) {
		subKind = "ServiceAccount"
	}

	sub := []rbac.Subject{
		{
			APIGroup:  "",
			Kind:      subKind,
			Name:      acc.Name,
			Namespace: common.AddonAgentInstallNamespace,
		},
	}

	crb := rbac.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s.proxy", acc.Name),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(acc, authenticationv1alpha1.GroupVersion.WithKind("Account")),
			},
		},
		Subjects: sub,
		RoleRef: rbac.RoleRef{
			APIGroup: rbac.GroupName,
			Kind:     "ClusterRole",
			Name:     common.GatewayProxyClusterRole,
		},
	}

	if strings.Contains(acc.Spec.Username, common.ServiceAccountPrefix) {
		crb.Name = fmt.Sprintf("ace.%s.proxy", acc.Name)
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

func (r *AccountReconciler) createImpersonateClusterRoleAndRoleBinding(ctx context.Context, acc *authenticationv1alpha1.Account) error {
	// impersonate clusterRole
	rules := []rbac.PolicyRule{
		{
			APIGroups: []string{"authentication.k8s.io"},
			Resources: []string{"userextras/" + kmapi.AceOrgIDKey, "userextras/" + kmapi.ClientOrgKey},
			Verbs:     []string{"impersonate"},
		},
	}

	if strings.Contains(acc.Spec.Username, common.ServiceAccountPrefix) {
		name, _, err := utils.ExtractServiceAccountNameAndNamespace(acc.Spec.Username)
		if err != nil {
			return err
		}

		rules = append(rules, rbac.PolicyRule{
			APIGroups:     []string{""},
			Resources:     []string{"serviceaccounts"},
			Verbs:         []string{"impersonate"},
			ResourceNames: []string{name},
		})
	} else {
		rules = append(rules, rbac.PolicyRule{
			APIGroups:     []string{""},
			Resources:     []string{"users"},
			Verbs:         []string{"impersonate"},
			ResourceNames: []string{acc.Spec.Username},
		})
	}

	cr := rbac.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: fmt.Sprintf("%s.impersonate", acc.Name),
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(acc, authenticationv1alpha1.GroupVersion.WithKind("Account")),
			},
		},
		Rules: rules,
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
			Name:      acc.Name,
			Namespace: common.AddonAgentInstallNamespace,
		},
	}

	crb := rbac.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: cr.Name, // creating cluster-rolebinding name with the same name of cluster-role
			OwnerReferences: []metav1.OwnerReference{
				*metav1.NewControllerRef(acc, authenticationv1alpha1.GroupVersion.WithKind("Account")),
			},
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

// updateConditions adds or updates a condition in the conditions array.
func (r *AccountReconciler) updateConditions(conditions []kmapi.Condition, conditionType kmapi.ConditionType, message string) []kmapi.Condition {
	now := metav1.Now()
	for i, condition := range conditions {
		if condition.Type == conditionType {
			conditions[i].Status = metav1.ConditionStatus(core.ConditionTrue)
			conditions[i].LastTransitionTime = now
			conditions[i].Message = message
			return conditions
		}
	}

	return append(conditions, kmapi.Condition{
		Type:               conditionType,
		Status:             metav1.ConditionStatus(core.ConditionTrue),
		LastTransitionTime: now,
		Reason:             string(conditionType),
		Message:            message,
	})
}

func (r *AccountReconciler) setStatusFailed(ctx context.Context, acc *authenticationv1alpha1.Account, err error) error {
	// Re-fetch the latest version of the Account object
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(acc), acc); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get latest account object: %w", err)
	}

	acc.Status.Phase = authenticationv1alpha1.AccountPhaseFailed
	setAccountType(acc)

	acc.Status.Conditions = r.updateConditions(acc.Status.Conditions, "ReconciliationFailed", err.Error())
	if updateErr := r.Client.Status().Update(ctx, acc); updateErr != nil {
		return fmt.Errorf("failed to update status to Failed: %w", updateErr)
	}
	return err
}

func (r *AccountReconciler) setStatusSuccess(ctx context.Context, acc *authenticationv1alpha1.Account, message string) error {
	// Re-fetch the latest version of the Account object
	if err := r.Client.Get(ctx, client.ObjectKeyFromObject(acc), acc); err != nil && !errors.IsNotFound(err) {
		return fmt.Errorf("failed to get latest account object: %w", err)
	}

	acc.Status.Phase = authenticationv1alpha1.AccountPhaseCurrent
	acc.Status.Conditions = r.updateConditions(acc.Status.Conditions, "ReconciliationSuccessful", message)
	setAccountType(acc)
	acc.Status.ServiceAccountRef = &core.LocalObjectReference{
		Name: acc.Name,
	}

	// Update or add a successful condition
	acc.Status.Conditions = r.updateConditions(acc.Status.Conditions, "ReconciliationSuccessful", message)
	if updateErr := r.Client.Status().Update(ctx, acc); updateErr != nil {
		return fmt.Errorf("failed to update status to Current: %w", updateErr)
	}
	return nil
}

func setAccountType(acc *authenticationv1alpha1.Account) {
	if strings.Contains(acc.Spec.Username, common.ServiceAccountPrefix) {
		acc.Status.Type = authenticationv1alpha1.AccountTypeServiceAccount
	} else {
		acc.Status.Type = authenticationv1alpha1.AccountTypeUser
	}
}

// SetupWithManager sets up the controller with the Manager.
func (r *AccountReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&authenticationv1alpha1.Account{}).
		Complete(r)
}
