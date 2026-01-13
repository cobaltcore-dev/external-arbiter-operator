// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	RemoteClusterExistsConditionType     = "RemoteClusterExists"
	RemoteClusterReadyConditionType      = "RemoteClusterReady"
	CephClusterExistsConditionType       = "CephClusterExists"
	CephClusterReadyConditionType        = "CephClusterReady"
	CephClusterConfiguredConditionType   = "CephClusterConfigured"
	MonitorDeploymentExistsConditionType = "MonitorDeploymentExists"
	MonitorDeploymentReadyConditionType  = "MonitorDeploymentReady"
	ArbiterDeploymentExistsConditionType = "ArbiterDeploymentExists"
	ArbiterDeploymentReadyConditionType  = "ArbiterDeploymentReady"

	RemoteArbiterInitState        RemoteArbiterState = "Init"
	RemoteArbiterProgressingState RemoteArbiterState = "Progressing"
	RemoteArbiterErrorState       RemoteArbiterState = "Error"
	RemoteArbiterReadyState       RemoteArbiterState = "Ready"
	RemoteArbiterDeletingState    RemoteArbiterState = "Deleting"

	NamespacedReferenceSeparator = "/"
)

type RemoteArbiterState string

// NamespacedReference points to resource in particular namespace
type NamespacedReference struct {
	// Namespace is a referred resource namespace
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// Namespace is a referred resource name
	// +required
	Name string `json:"name,omitempty"`
}

func (r NamespacedReference) String() string {
	return r.Namespace + NamespacedReferenceSeparator + r.Name
}

// PodConfiguration allows to configure particular aspects of running pod
type PodConfiguration struct {
	// Affinity allows to configure pod affinity
	// +optional
	Affinity *corev1.Affinity `json:"affinity,omitempty"`
	// Resources allows to configure computing resource consumption
	// +optional
	Resources *corev1.ResourceRequirements `json:"resources,omitempty"`
	// NodeSelector allows to specify node labels
	// +optional
	NodeSelector map[string]string `json:"nodeSelector,omitempty"`
}

// RemoteClusterConfiguration allows to refer RemoteCluster instance, or to define its Spec
// +kubebuilder:validation:ExactlyOneOf=name;spec
type RemoteClusterConfiguration struct {
	// Spec allows to define RemoteCluster spec in place.
	// In this case RemoteArbiter controller will take care of RemoteCluster creation
	// +optional
	Spec *RemoteClusterSpec `json:"spec,omitempty"`
	// Name allows to refer RemoteCluster in the same namespace
	// +optional
	Name string `json:"name,omitempty"`
}

// ServiceConfiguration allows to configure Service
type ServiceConfiguration struct {
	// Type allows to select service type
	// +default="ClusterIP"
	// +example="ClusterIP"
	// +optional
	Type corev1.ServiceType
}

// RemoteArbiterSpec defines the desired state of RemoteArbiter
type RemoteArbiterSpec struct {

	// Deployment allows to alter configuratio of RemoteArbiter deployment
	// +optional
	Deployment PodConfiguration `json:"deployment,omitempty"`

	// Service allows to configure arbiter exposure via service
	// +optional
	Service *string `json:"service,omitempty"`

	// CheckInterval defines a reconcile period for RemoteArbiter, to check its health
	// +default="1m"
	// +example="1m"
	// +optional
	CheckInterval *Interval `json:"checkInterval,omitempty"`

	// CephCluster refers to CephCluster resource maanaged by Rook
	// +required
	CephCluster NamespacedReference `json:"cephCluster,omitempty"`

	// RemoteCluster refers to RemoteCluster resource or defines its spec
	// +required
	RemoteCluster RemoteClusterConfiguration `json:"remoteCluster,omitempty"`

	// MonIDPrefix allows to set up a Ceph monitor ID prefix, to avoid collisions with default mon ID naming algorithm.
	// +default="ext-"
	// +example="ext-"
	// +optional
	MonIDPrefix string `json:"monIdPrefix,omitempty"`
}

// RemoteArbiterStatus defines the observed state of RemoteArbiter.
type RemoteArbiterStatus struct {
	// State represents current reconcile state
	State RemoteArbiterState `json:"state,omitempty"`
	// Message provides an info about current state
	Message string `json:"message,omitempty"`
	// MonID shows monitor ID reserved for RemoteArbiter
	MonID string `json:"monId,omitempty"`
	// Conditions are showing reconcile steps and their execution results
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="Mon ID",type=string,JSONPath=`.status.monId`,description="State"
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`,description="State"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=`.status.message`,description="Message"
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
