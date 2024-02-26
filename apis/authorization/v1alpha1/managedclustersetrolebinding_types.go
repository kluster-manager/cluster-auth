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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// ManagedClusterSetRoleBinding is the Schema for the managedclustersetrolebindings API
type ManagedClusterSetRoleBinding struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	// Subjects holds references to the objects the role applies to.
	// +optional
	Subjects []Subject `json:"subjects,omitempty"`

	// RoleRef can reference a Role in the current namespace or a ClusterRole in the global namespace.
	// If the RoleRef cannot be resolved, the Authorizer must return an error.
	// This field is immutable.
	RoleRef ClusterSetRoleRef `json:"roleRef"`

	// This field is immutable.
	ClusterSetRef ClusterSetRef `json:"clusterSetRef"`
}

// ClusterSetRoleRef contains information that points to the role being used
// +structType=atomic
type ClusterSetRoleRef struct {
	// APIGroup is the group for the resource being referenced
	APIGroup string `json:"apiGroup"`
	// Kind is the type of resource being referenced
	Kind string `json:"kind"`
	// Name is the name of resource being referenced
	Name string `json:"name"`
	// Namespaces are the name of the namespaces for Roles
	Namespaces []string `json:"namespaces"`
}

// ClusterSetRef contains information that points to the ManagedClusterSet being used
// +structType=atomic
type ClusterSetRef struct {
	// Name is the name of resource being referenced
	Name string `json:"name"`
}

//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+kubebuilder:object:root=true

// ManagedClusterSetRoleBindingList contains a list of ManagedClusterSetRoleBinding
type ManagedClusterSetRoleBindingList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []ManagedClusterSetRoleBinding `json:"items"`
}

func init() {
	SchemeBuilder.Register(&ManagedClusterSetRoleBinding{}, &ManagedClusterSetRoleBindingList{})
}
