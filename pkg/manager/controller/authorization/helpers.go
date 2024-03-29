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
	"k8s.io/apimachinery/pkg/runtime"
	cu "kmodules.xyz/client-go/client"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// generateManifestWorkName returns the ManifestWork name for a given ClusterPermission.
// It uses the ClusterPermission name with the suffix of the first 5 characters of the UID
func generateManifestWorkName(name string) string {
	return name + "-auth"
}

// buildManifestWork wraps the payloads in a ManifestWork
func buildManifestWork(clusterRoleBinding []rbac.ClusterRoleBinding, manifestWorkName string, ns string) *workv1.ManifestWork {
	var manifests []workv1.Manifest

	if len(clusterRoleBinding) > 0 {
		for i := range clusterRoleBinding {
			manifests = append(manifests, workv1.Manifest{RawExtension: runtime.RawExtension{Object: &clusterRoleBinding[i]}})
		}
	}

	return &workv1.ManifestWork{
		ObjectMeta: metav1.ObjectMeta{
			Name:      manifestWorkName,
			Namespace: ns,
		},
		Spec: workv1.ManifestWorkSpec{
			Workload: workv1.ManifestsTemplate{
				Manifests: manifests,
			},
		},
	}
}

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

func generateManifestWorkPayload(managedCRBList *v1alpha1.ManagedClusterRoleBindingList) []rbac.ClusterRoleBinding {
	var clusterRoleBindings []rbac.ClusterRoleBinding

	for _, managedCRB := range managedCRBList.Items {
		// now give the managed-serviceaccount permission to impersonate user
		sub := []rbac.Subject{
			{
				APIGroup: "",
				Kind:     "User",
				Name:     managedCRB.Subjects[0].Name,
			},
		}
		crb := rbac.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: rbac.SchemeGroupVersion.String(),
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: managedCRB.Name,
				Labels: map[string]string{
					"authentication.k8s.appscode.com/username": managedCRB.Subjects[0].Name,
				},
			},
			Subjects: sub,
			RoleRef: rbac.RoleRef{
				APIGroup: rbac.GroupName,
				Kind:     "ClusterRole",
				Name:     managedCRB.RoleRef.Name,
			},
		}
		clusterRoleBindings = append(clusterRoleBindings, crb)
	}

	return clusterRoleBindings
}
