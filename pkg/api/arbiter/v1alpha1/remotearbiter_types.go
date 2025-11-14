// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type NamespacedReference struct {
	Namespace string `json:"namespace,omitempty"`
	Name      string `json:"name,omitempty"`
}

type PodConfiguration struct {
	Affinity  *corev1.Affinity             `json:"affinity,omitempty"`
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
}

// RemoteArbiterSpec defines the desired state of RemoteArbiter
type RemoteArbiterSpec struct {
	// INSERT ADDITIONAL SPEC FIELDS - desired state of cluster
	// Important: Run "make" to regenerate code after modifying this file
	// The following markers will use OpenAPI v3 schema to validate the value
	// More info: https://book.kubebuilder.io/reference/markers/crd-validation.html
	Deployment PodConfiguration `json:"deployment,omitempty"`

	CephCluster NamespacedReference `json:"cephCluster,omitempty"`

	// foo is an example field of RemoteArbiter. Edit remotearbiter_types.go to remove/update
	// +optional

	RemoteCluster RemoteClusterSpec `json:"remoteCluster,omitempty"`
}

// RemoteArbiterStatus defines the observed state of RemoteArbiter.
type RemoteArbiterStatus struct {
	// INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
	// Important: Run "make" to regenerate code after modifying this file

	// For Kubernetes API conventions, see:
	// https://github.com/kubernetes/community/blob/master/contributors/devel/sig-architecture/api-conventions.md#typical-status-properties

	// conditions represent the current state of the RemoteArbiter resource.
	// Each condition has a unique type and reflects the status of a specific aspect of the resource.
	//
	// Standard condition types include:
	// - "Available": the resource is fully functional
	// - "Progressing": the resource is being created or updated
	// - "Degraded": the resource failed to reach or maintain its desired state
	//
	// The status of each condition is one of True, False, or Unknown.
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

	// spec defines the desired state of RemoteArbiter
	// +required
	Spec RemoteArbiterSpec `json:"spec"`

	// status defines the observed state of RemoteArbiter
	// +optional
	Status RemoteArbiterStatus `json:"status,omitempty,omitzero"`
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
