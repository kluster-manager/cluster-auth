package helper_controller

import (
	"context"
	"github.com/kluster-manager/cluster-auth/api/authorization/v1alpha1"
	"github.com/kluster-manager/cluster-auth/pkg/addon/agent/controller"
	rbacv1 "k8s.io/api/rbac/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	v1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

// generateManifestWorkName returns the ManifestWork name for a given ClusterPermission.
// It uses the ClusterPermission name with the suffix of the first 5 characters of the UID
func generateManifestWorkName(cluster *v1.ManagedCluster) string {
	return cluster.Name + "-auth"
}

// buildManifestWork wraps the payloads in a ManifestWork
func buildManifestWork(clusterRole []rbacv1.ClusterRole, clusterRoleBinding []rbacv1.ClusterRoleBinding, manifestWorkName string, ns string) *workv1.ManifestWork {
	var manifests []workv1.Manifest

	if len(clusterRole) > 0 {
		for i := range clusterRole {
			manifests = append(manifests, workv1.Manifest{RawExtension: runtime.RawExtension{Object: &clusterRole[i]}})
		}
	}

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

func generateManifestWorkPayload(c client.Client, ctx context.Context, managedCRList *v1alpha1.ManagedClusterRoleList) ([]rbacv1.ClusterRole, []rbacv1.ClusterRoleBinding, error) {
	var clusterRoles []rbacv1.ClusterRole
	var clusterRoleBindings []rbacv1.ClusterRoleBinding

	for _, managedCR := range managedCRList.Items {
		// impersonate clusterRole
		cr := rbacv1.ClusterRole{
			TypeMeta: metav1.TypeMeta{
				APIVersion: rbacv1.SchemeGroupVersion.String(),
				Kind:       "ClusterRole",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: managedCR.Name,
				Labels: map[string]string{
					"authentication.k8s.appscode.com/username":               managedCR.Labels["authentication.k8s.appscode.com/username"],
					"authentication.k8s.appscode.com/managed-serviceaccount": "managedserviceaccount-" + managedCR.Labels["authentication.k8s.appscode.com/username"],
				},
			},
			Rules: managedCR.Rules,
		}

		clusterRoles = append(clusterRoles, cr)

		// now give the managed-serviceaccount permission to impersonate user
		sub := []rbacv1.Subject{
			{
				APIGroup:  "",
				Kind:      "ServiceAccount",
				Name:      managedCR.Labels["authentication.k8s.appscode.com/managed-serviceaccount"],
				Namespace: controller.ManagedServiceAccountNamespace,
			},
		}
		crb := rbacv1.ClusterRoleBinding{
			TypeMeta: metav1.TypeMeta{
				APIVersion: rbacv1.SchemeGroupVersion.String(),
				Kind:       "ClusterRoleBinding",
			},
			ObjectMeta: metav1.ObjectMeta{
				Name: managedCR.Name,
				Labels: map[string]string{
					"authentication.k8s.appscode.com/username":               managedCR.Labels["authentication.k8s.appscode.com/username"],
					"authentication.k8s.appscode.com/managed-serviceaccount": "managedserviceaccount-" + managedCR.Labels["authentication.k8s.appscode.com/username"],
				},
			},
			Subjects: sub,
			RoleRef: rbacv1.RoleRef{
				APIGroup: rbacv1.GroupName,
				Kind:     "ClusterRole",
				Name:     managedCR.Name,
			},
		}

		clusterRoleBindings = append(clusterRoleBindings, crb)
	}

	return clusterRoles, clusterRoleBindings, nil
}
