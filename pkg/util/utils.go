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

package util

import (
	authenticationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authentication/v1alpha1"
	authorizationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authorization/v1alpha1"

	rbac "k8s.io/api/rbac/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	v1 "open-cluster-management.io/api/cluster/v1"
	ocmkl "open-cluster-management.io/api/operator/v1"
	workv1 "open-cluster-management.io/api/work/v1"
	mSA "open-cluster-management.io/managed-serviceaccount/apis/authentication/v1alpha1"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var scheme = runtime.NewScheme()

func GetKubeClient(kubeConfig *rest.Config) (client.Client, error) {
	_ = clientgoscheme.AddToScheme(scheme)
	_ = authenticationv1alpha1.AddToScheme(scheme)
	_ = authorizationv1alpha1.AddToScheme(scheme)
	_ = mSA.AddToScheme(scheme)
	_ = rbac.AddToScheme(scheme)
	_ = v1.Install(scheme)
	_ = workv1.Install(scheme)
	_ = ocmkl.Install(scheme)
	return client.New(kubeConfig, client.Options{Scheme: scheme})
}
