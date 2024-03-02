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
	errors2 "errors"
	"time"

	authenticationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authentication/v1alpha1"
	authorizationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authorization/v1alpha1"
	"github.com/kluster-manager/cluster-auth/pkg/common"

	core "k8s.io/api/core/v1"
	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/rest"
	cu "kmodules.xyz/client-go/client"
	"kmodules.xyz/client-go/meta"
	"kmodules.xyz/client-go/tools/clientcmd"
	workv1 "open-cluster-management.io/api/work/v1"
	managedsaapi "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1alpha1"
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

	ns := managedCRB.GetNamespace()
	if err = createManagedServiceAccount(ctx, ns, r.Client); err != nil {
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

	if err = createManagedClusterRole(ctx, r.Client, ns, managedCRB); err != nil {
		return reconcile.Result{}, err
	}

	managedCRBList := &authorizationv1alpha1.ManagedClusterRoleBindingList{}
	if err := r.Client.List(ctx, managedCRBList, &client.ListOptions{Namespace: ns}); err != nil {
		return reconcile.Result{}, err
	}
	clusterRoleBinding := generateManifestWorkPayload(managedCRBList)

	manifestWorkName := generateManifestWorkName(ns)
	manifestWork := buildManifestWork(clusterRoleBinding, manifestWorkName, ns)

	if len(manifestWork.Spec.Workload.Manifests) == 0 {
		return reconcile.Result{}, errors2.New("manifestwork item cant be 0")
	}
	var mw workv1.ManifestWork
	err = r.Get(ctx, types.NamespacedName{Name: manifestWorkName, Namespace: ns}, &mw)
	if errors.IsNotFound(err) {
		err = r.Client.Create(ctx, manifestWork)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else if err == nil {
		mw.Spec = manifestWork.Spec
		err = r.Client.Update(ctx, &mw)
		if err != nil {
			return ctrl.Result{}, err
		}
	} else {
		return ctrl.Result{}, err
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
			Name:     common.GatewayRole,
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
	sa := &core.ServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "ace-sa-" + managedCRB.Subjects[0].Name,
			Namespace: common.AddonAgentInstallNamespace,
		},
	}
	_, err := cu.CreateOrPatch(context.Background(), c, sa, func(obj client.Object, createOp bool) client.Object {
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
	ns := &core.Namespace{
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

func createManagedServiceAccount(ctx context.Context, ns string, c client.Client) error {
	managedSA := &managedsaapi.ManagedServiceAccount{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "managedserviceaccount-" + ns,
			Namespace: ns,
		},
		Spec: managedsaapi.ManagedServiceAccountSpec{
			Rotation: managedsaapi.ManagedServiceAccountRotation{
				Enabled: true,
				Validity: metav1.Duration{
					Duration: time.Hour * 720,
				},
			},
		},
	}

	msa := &managedsaapi.ManagedServiceAccount{}
	err := c.Get(ctx, types.NamespacedName{Name: managedSA.Name, Namespace: managedSA.Namespace}, msa)
	if errors.IsNotFound(err) {
		_, err = cu.CreateOrPatch(context.Background(), c, managedSA, func(obj client.Object, createOp bool) client.Object {
			in := obj.(*managedsaapi.ManagedServiceAccount)
			in.Spec = managedSA.Spec
			return in
		})
		if err != nil {
			return err
		}
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
				"authentication.k8s.appscode.com/username": usr.Name,
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

	err := c.Get(ctx, types.NamespacedName{Namespace: managedCR.Namespace, Name: managedCR.Name}, managedCR)
	if errors.IsNotFound(err) {
		_, err := cu.CreateOrPatch(context.Background(), c, managedCR, func(obj client.Object, createOp bool) client.Object {
			in := obj.(*authorizationv1alpha1.ManagedClusterRole)
			in.Labels = meta.OverwriteKeys(in.Labels, managedCR.Labels)
			in.Rules = managedCR.Rules
			return in
		})
		if err != nil {
			return err
		}
	} else {
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
