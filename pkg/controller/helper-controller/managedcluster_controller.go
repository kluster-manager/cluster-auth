// /*
// Copyright 2024.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//	http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.
// */
package helper_controller

import (
	"context"
	errors2 "errors"
	"github.com/kluster-manager/cluster-auth/api/authorization/v1alpha1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	v1 "open-cluster-management.io/api/cluster/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"gomodules.xyz/oneliners"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ManagedClusterReconciler reconciles a ManagedCluster object
type ManagedClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

func (r *ManagedClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Start reconciling")

	managedCluster := &v1.ManagedCluster{}
	if err := r.Client.Get(ctx, req.NamespacedName, managedCluster); err != nil {
		return reconcile.Result{}, err
	}

	// get list of managedClusterRole
	managedCRList := &v1alpha1.ManagedClusterRoleList{}
	if err := r.Client.List(ctx, managedCRList, &client.ListOptions{Namespace: managedCluster.Name}); err != nil {
		return reconcile.Result{}, err
	}
	clusterRole, clusterRoleBinding, err := generateManifestWorkPayload(r.Client, ctx, managedCRList)
	oneliners.PrettyJson(clusterRole, "ClusterRole")
	oneliners.PrettyJson(clusterRoleBinding, "ClusterRoleBinding")

	if err != nil {
		return reconcile.Result{}, err
	}

	manifestWorkName := generateManifestWorkName(managedCluster)
	manifestWork := buildManifestWork(clusterRole, clusterRoleBinding, manifestWorkName, managedCluster.Name)

	if len(manifestWork.Spec.Workload.Manifests) == 0 {
		return reconcile.Result{}, errors2.New("manifest item can't be zero")
	}
	var mw workv1.ManifestWork
	err = r.Get(ctx, types.NamespacedName{Name: manifestWorkName, Namespace: managedCluster.Name}, &mw)
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

// SetupWithManager sets up the controller with the Manager.
func (r *ManagedClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1.ManagedCluster{}).Watches(&v1.ManagedCluster{}, &handler.EnqueueRequestForObject{}).
		Complete(r)
}
