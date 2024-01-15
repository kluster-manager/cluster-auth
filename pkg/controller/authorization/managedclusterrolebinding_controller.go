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

package authorization

import (
	"context"
	authenticationv1alpha1 "github.com/kluster-manager/cluster-auth/api/authentication/v1alpha1"
	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	mSA "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"
	"time"

	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	authorizationv1alpha1 "github.com/kluster-manager/cluster-auth/api/authorization/v1alpha1"
	cg "kmodules.xyz/client-go/client"
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

	ns := managedCRB.GetNamespace()
	if err = createManagedServiceAccount(r.Client, ns, managedCRB); err != nil {
		return reconcile.Result{}, err

	}

	if err = createManagedClusterRole(ctx, r.Client, ns, managedCRB); err != nil {
		return reconcile.Result{}, err
	}
	return reconcile.Result{}, nil
}

func createManagedServiceAccount(c client.Client, ns string, managedCRB *authorizationv1alpha1.ManagedClusterRoleBinding) error {
	managedSA := &mSA.ManagedServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "managedserviceaccount-" + managedCRB.Subjects[0].Name,
			Namespace: ns,
		},
		Spec: mSA.ManagedServiceAccountSpec{
			Rotation: mSA.ManagedServiceAccountRotation{
				Enabled: true,
				Validity: metav1.Duration{
					Duration: time.Hour * 720,
				},
			},
		},
	}

	_, err := cg.CreateOrPatch(context.Background(), c, managedSA, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*mSA.ManagedServiceAccount)
		in.Spec = managedSA.Spec
		return in
	})
	if err != nil {
		return err
	}

	return nil
}

func createManagedClusterRole(ctx context.Context, c client.Client, ns string, managedCRB *authorizationv1alpha1.ManagedClusterRoleBinding) error {
	usr := &authenticationv1alpha1.User{}
	if err := c.Get(ctx, types.NamespacedName{Name: managedCRB.Subjects[0].Name}, usr); err != nil {
		return err
	}

	managedCR := &authorizationv1alpha1.ManagedClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ace-user-" + usr.Name,
			Namespace: ns,
			Labels: map[string]string{
				"authentication.k8s.appscode.com/username":               usr.Name,
				"authentication.k8s.appscode.com/managed-serviceaccount": "managedserviceaccount-" + managedCRB.Subjects[0].Name,
			},
		},
		Rules: []rbac.PolicyRule{
			{
				APIGroups:     []string{""},
				Resources:     []string{"users"},
				Verbs:         []string{"impersonate"},
				ResourceNames: []string{usr.Name},
			},
		},
	}

	_, err := cg.CreateOrPatch(context.Background(), c, managedCR, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*authorizationv1alpha1.ManagedClusterRole)
		in.Rules = managedCR.Rules
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
