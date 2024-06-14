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

package utils

import (
	authorizationv1alpha1 "github.com/kluster-manager/cluster-auth/apis/authorization/v1alpha1"
)

func GetUserIDAndHubOwnerIDFromLabelValues(object *authorizationv1alpha1.ManagedClusterRoleBinding) (string, string) {
	labels := object.GetLabels()

	userID, hubOwnerID := "", ""
	if labelValue, ok := labels["authentication.k8s.appscode.com/user"]; ok {
		userID = labelValue
	}
	if labelValue, ok := labels["authentication.k8s.appscode.com/hub-owner"]; ok {
		hubOwnerID = labelValue
	}

	return userID, hubOwnerID
}
