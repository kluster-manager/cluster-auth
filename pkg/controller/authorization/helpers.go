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

	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	cg "kmodules.xyz/client-go/client"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// generateManifestWorkName returns the ManifestWork name for a given ClusterPermission.
// It uses the ClusterPermission name with the suffix of the first 5 characters of the UID
func generateManifestWorkName(name string) string {
	return name + "-auth"
}

// buildManifestWork wraps the payloads in a ManifestWork
func buildManifestWork(clusterRoleBinding []rbacv1.ClusterRoleBinding, manifestWorkName string, ns string) *workv1.ManifestWork {
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

func createClusterRoleAndClusterRoleBindingToImpersonate(c client.Client, ctx context.Context, managedCR *v1alpha1.ManagedClusterRole) error {
	// impersonate clusterRole
	cr := &rbacv1.ClusterRole{
		ObjectMeta: metav1.ObjectMeta{
			Name: managedCR.Name,
		},
		Rules: managedCR.Rules,
	}

	_, err := cg.CreateOrPatch(context.Background(), c, cr, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*rbacv1.ClusterRole)
		in.Rules = cr.Rules
		return in
	})
	if err != nil {
		return err
	}

	// now give the serviceaccount permission to impersonate user
	sub := []rbacv1.Subject{
		{
			APIGroup:  "",
			Kind:      "ServiceAccount",
			Name:      "ace-sa-" + managedCR.Labels["authentication.k8s.appscode.com/username"],
			Namespace: ClusterAuthNamespace,
		},
	}
	crb := &rbacv1.ClusterRoleBinding{
		ObjectMeta: metav1.ObjectMeta{
			Name: managedCR.Name,
		},
		Subjects: sub,
		RoleRef: rbacv1.RoleRef{
			APIGroup: rbacv1.GroupName,
			Kind:     "ClusterRole",
			Name:     managedCR.Name,
		},
	}

	_, err = cg.CreateOrPatch(context.Background(), c, crb, func(obj client.Object, createOp bool) client.Object {
		in := obj.(*rbacv1.ClusterRoleBinding)
		in.Subjects = crb.Subjects
		in.RoleRef = crb.RoleRef
		return in
	})
	if err != nil {
		return err
	}

	return nil
}

func generateManifestWorkPayload(c client.Client, ctx context.Context, managedCRBList *v1alpha1.ManagedClusterRoleBindingList) ([]rbacv1.ClusterRoleBinding, error) {
	var clusterRoleBindings []rbacv1.ClusterRoleBinding

	for _, managedCRB := range managedCRBList.Items {
		// now give the managed-serviceaccount permission to impersonate user
		sub := []rbacv1.Subject{
			{
				APIGroup: "",
				Kind:     "User",
				Name:     managedCRB.Subjects[0].Name,
			},
		}
		crb := rbacv1.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: rbacv1.SchemeGroupVersion.String(),
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: managedCRB.Name,
				Labels: map[string]string{
					"authentication.k8s.appscode.com/username": managedCRB.Subjects[0].Name,
				},
			},
			Subjects: sub,
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     managedCRB.RoleRef.Name,
			},
		}

		clusterRoleBindings = append(clusterRoleBindings, crb)
	}

	return clusterRoleBindings, nil
}
