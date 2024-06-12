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
