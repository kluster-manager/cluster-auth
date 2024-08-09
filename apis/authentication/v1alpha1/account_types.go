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
	"fmt"

	core "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	kmapi "kmodules.xyz/client-go/api/v1"
)

// +kubebuilder:validation:Enum=User;ServiceAccount
type AccountType string

const (
	AccountTypeUser           AccountType = "User"
	AccountTypeServiceAccount AccountType = "ServiceAccount"
)

// AccountSpec defines the desired state of Account
type AccountSpec struct {
	// The name that uniquely identifies this user among all active users.
	// +optional
	Username string `json:"username,omitempty"`
	// A unique value that identifies this user across time. If this user is
	// deleted and another user by the same name is added, they will have
	// different UIDs.
	// +optional
	UID string `json:"uid,omitempty"`
	// The names of groups this user is a part of.
	// +optional
	Groups []string `json:"groups,omitempty"`
	// Any additional information provided by the authenticator.
	// +optional
	Extra map[string]ExtraValue `json:"extra,omitempty"`

	TokenGeneration *int64 `json:"tokenGeneration,omitempty"`
}

// ExtraValue masks the value so protobuf can generate
// +protobuf.nullable=true
// +protobuf.options.(gogoproto.goproto_stringer)=false
type ExtraValue []string

func (t ExtraValue) String() string {
	return fmt.Sprintf("%v", []string(t))
}

type AccountPhase string

const (
	AccountPhaseInProgress AccountPhase = "InProgress"
	AccountPhaseCurrent    AccountPhase = "Current"
	AccountPhaseFailed     AccountPhase = "Failed"
)

// AccountStatus defines the observed state of Account
type AccountStatus struct {
	// Phase indicates the state this Vault cluster jumps in.
	// +optional
	Phase AccountPhase `json:"phase,omitempty"`
	// Represents the latest available observations of a GrafanaDashboard current state.
	// +optional
	Conditions []kmapi.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the most recent generation observed for this resource. It corresponds to the
	// resource's generation, which is updated on mutation by the API Server.
	// +optional
	ObservedGeneration      int64 `json:"observedGeneration,omitempty"`
	ObservedTokenGeneration int64 `json:"observedTokenGeneration,omitempty"`
	// +optional
	ServiceAccountRef *core.LocalObjectReference `json:"serviceAccountRef,omitempty"`
	Type              AccountType                `json:"type"`
}

// Account is the Schema for the users API

//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+kubebuilder:object:root=true
//+kubebuilder:subresource:status
//+kubebuilder:resource:scope=Cluster

// +kubebuilder:printcolumn:name="Username",type="string",JSONPath=".spec.username"
// +kubebuilder:printcolumn:name="Type",type="string",JSONPath=".status.type"
// +kubebuilder:printcolumn:name="Status",type="string",JSONPath=".status.phase"
// +kubebuilder:printcolumn:name="Age",type="date",JSONPath=".metadata.creationTimestamp"
type Account struct {
	metav1.TypeMeta   `json:",inline"`
	metav1.ObjectMeta `json:"metadata,omitempty"`

	Spec   AccountSpec   `json:"spec,omitempty"`
	Status AccountStatus `json:"status,omitempty"`
}

//+k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object
//+kubebuilder:object:root=true

// AccountList contains a list of Account
type AccountList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []Account `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Account{}, &AccountList{})
}
