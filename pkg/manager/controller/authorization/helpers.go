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

	"github.com/kluster-manager/cluster-auth/apis/authorization/v1alpha1"
	"github.com/kluster-manager/cluster-auth/pkg/common"

	rbac "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cu "kmodules.xyz/client-go/client"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

func createClusterRoleAndClusterRoleBindingToImpersonate(ctx context.Context, c client.Client, managedCR *v1alpha1.ManagedClusterRole) error {
	// impersonate clusterRole
	cr := &rbac.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: managedCR.Name,
		},
		Rules: managedCR.Rules,
	}

	_, err := cu.CreateOrPatch(ctx, c, cr, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*rbac.ClusterRole)
		in.Rules = cr.Rules
		return in
	})
	if err != nil {
		return err
	}

	// now give the serviceaccount permission to impersonate user
	sub := []rbac.Subject{
		{
			APIGroup:  "",
			Kind:      "ServiceAccount",
			Name:      "ace-sa-" + managedCR.Labels["authentication.k8s.appscode.com/username"],
			Namespace: common.AddonAgentInstallNamespace,
		},
	}
	crb := &rbac.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: managedCR.Name,
		},
		Subjects: sub,
		RoleRef: rbac.RoleRef{
			APIGroup: rbac.GroupName,
			Kind:     "ClusterRole",
			Name:     managedCR.Name,
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
