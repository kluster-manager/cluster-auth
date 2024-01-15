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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	cg "kmodules.xyz/client-go/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"k8s.io/apimachinery/pkg/runtime"
	clusterclientset "open-cluster-management.io/api/client/cluster/clientset/versioned"
	clusterapiv1beta2 "open-cluster-management.io/api/cluster/v1beta2"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	authorizationv1alpha1 "github.com/kluster-manager/cluster-auth/api/authorization/v1alpha1"
)

// ManagedClusterSetRoleBindingReconciler reconciles a ManagedClusterSetRoleBinding object
type ManagedClusterSetRoleBindingReconciler struct {
	client.Client
	Scheme        *runtime.Scheme
	ClusterClient clusterclientset.Clientset
}

//+kubebuilder:rbac:groups=authorization.k8s.appscode.com,resources=managedclustersetrolebindings,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=authorization.k8s.appscode.com,resources=managedclustersetrolebindings/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=authorization.k8s.appscode.com,resources=managedclustersetrolebindings/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ManagedClusterSetRoleBinding object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.16.3/pkg/reconcile
func (r *ManagedClusterSetRoleBindingReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Start reconciling")

	managedCSRB := &authorizationv1alpha1.ManagedClusterSetRoleBinding{}
	err := r.Client.Get(ctx, req.NamespacedName, managedCSRB)
	if err != nil {
		return reconcile.Result{}, err
	}

	managedClusterSet, err := r.ClusterClient.ClusterV1beta2().ManagedClusterSets().Get(ctx, managedCSRB.ClusterSetRef.Name, metav1.GetOptions{})
	if err != nil {
		return reconcile.Result{}, err
	}

	sel, err := clusterapiv1beta2.BuildClusterSelector(managedClusterSet)
	clusters, err := r.ClusterClient.ClusterV1().ManagedClusters().List(ctx, metav1.ListOptions{LabelSelector: sel.String()})
	if err != nil {
		return reconcile.Result{}, err
	}

	// create managedClusterRoleBinding for every cluster of this clusterSet
	for _, cluster := range clusters.Items {
		managedCRB := &authorizationv1alpha1.ManagedClusterRoleBinding{
			ObjectMeta: metav1.ObjectMeta{
				Name:      managedCSRB.Name + "-" + cluster.Name,
				Namespace: cluster.Name,
			},
			Subjects: managedCSRB.Subjects,
			RoleRef:  authorizationv1alpha1.ClusterRoleRef(managedCSRB.RoleRef),
		}

		_, err = cg.CreateOrPatch(context.Background(), r.Client, managedCRB, func(obj client.Object, createOp bool) client.Object {
			in := obj.(*authorizationv1alpha1.ManagedClusterRoleBinding)
			in.Subjects = managedCRB.Subjects
			in.RoleRef = managedCRB.RoleRef
			return in
		})
		if err != nil {
			return reconcile.Result{}, err
		}
	}

	return reconcile.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedClusterSetRoleBindingReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&authorizationv1alpha1.ManagedClusterSetRoleBinding{}).
		Complete(r)
}
