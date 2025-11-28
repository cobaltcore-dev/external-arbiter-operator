// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	RemoteClusterExistsConditionType   = "RemoteClusterExists"
	RemoteClusterReadyConditionType    = "RemoteClusterReady"
	CephClusterExistsConditionType     = "CephClusterExists"
	CephClusterReadyConditionType      = "CephClusterReady"
	ComponentsCreatedConditionType     = "ComponentsCreated"
	CephClusterConfiguredConditionType = "CephClusterConfigured"
	ArbiterHealthyConditionType        = "ArbiterHealthy"

	RemoteArbiterInitState        RemoteArbiterState = "Init"
	RemoteArbiterProgressingState RemoteArbiterState = "Progressing"
	RemoteArbiterErrorState       RemoteArbiterState = "Error"
	RemoteArbiterReadyState       RemoteArbiterState = "Ready"
	RemoteArbiterDeletingState    RemoteArbiterState = "Deleting"
)

type RemoteArbiterState string

type NamespacedReference struct {
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// +required
	Name string `json:"name,omitempty"`
}

type PodConfiguration struct {
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// +kubebuilder:validation:ExactlyOneOf=name;spec
type RemoteClusterConfiguration struct {
	// +optional
	Name string `json:"name,omitempty"`
	// +optional
	Spec RemoteClusterSpec `json:"spec,omitempty"`
}

// RemoteArbiterSpec defines the desired state of RemoteArbiter
type RemoteArbiterSpec struct {

	// +optional
	Deployment PodConfiguration `json:"deployment,omitempty"`

	// +required
	CephCluster NamespacedReference `json:"cephCluster,omitempty"`

	// +required
	RemoteCluster RemoteClusterConfiguration `json:"remoteCluster,omitempty"`
	// +default="1m"
	// +example="1m"
	// +optional
	CheckInterval Interval `json:"checkInterval,omitempty"`
}

// RemoteArbiterStatus defines the observed state of RemoteArbiter.
type RemoteArbiterStatus struct {
	State   RemoteArbiterState `json:"state,omitempty"`
	Message string             `json:"message,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// RemoteArbiter is the Schema for the remotearbiters API
type RemoteArbiter struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// status defines the observed state of RemoteArbiter
	// +optional
	Status RemoteArbiterStatus `json:"status,omitempty,omitzero"`

	// spec defines the desired state of RemoteArbiter
	// +required
	Spec RemoteArbiterSpec `json:"spec"`
}

// +kubebuilder:object:root=true

// RemoteArbiterList contains a list of RemoteArbiter
type RemoteArbiterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemoteArbiter `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RemoteArbiter{}, &RemoteArbiterList{})
}
