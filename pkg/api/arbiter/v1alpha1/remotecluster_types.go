// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"encoding/json"
	"time"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	ConfigAvailableConditionType      = "ConfigAvailable"
	ConfigValidConditionType          = "ConfigValid"
	ClusterReachableConditionType     = "ClusterReachable"
	HasEnoughPermissionsConditionType = "HasEnoughPermissions"

	RemoteClusterInitState        RemoteClusterState = "Init"
	RemoteClusterProgressingState RemoteClusterState = "Progressing"
	RemoteClusterErrorState       RemoteClusterState = "Error"
	RemoteClusterReadyState       RemoteClusterState = "Ready"
	RemoteClusterDeletingState    RemoteClusterState = "Deleting"
)

type RemoteClusterState string

type KubeconfigSecretSource struct {
	// Name is a name of a secret resource in the same namespace
	// +optional
	// +default="matches RemoteCluster name"
	// +example="ceph-remote-cluster"
	Name string `json:"name,omitempty"`
	// Key is a key in a referred secret
	// +optional
	// +default="kubeconfig.yaml"
	// +example="kubeconfig.yaml"
	Key string `json:"key,omitempty"`
}

// Interval is a wrapper struct for time.Duration, used to provide Marsahlling funcs
// +kubebuilder:validation:Type=string
type Interval struct {
	time.Duration
}

func (r Interval) MarshalJSON() ([]byte, error) {
	return json.Marshal(r.String())
}

func (r *Interval) UnmarshalJSON(bytes []byte) error {
	var durationString string
	if err := json.Unmarshal(bytes, &durationString); err != nil {
		return err
	}
	var err error
	r.Duration, err = time.ParseDuration(durationString)
	if err != nil {
		return err
	}
	return nil
}

// RemoteClusterSpec defines the desired state of RemoteCluster
type RemoteClusterSpec struct {
	// Timeout allows to define request deadline for remote client
	// +default="10s"
	// +example="10s"
	// +optional
	Timeout *Interval `json:"timeout,omitempty"`
	// CheckInterval defines a reconcile period for RemoteCluster, to check its health
	// +default="1m"
	// +example="1m"
	// +optional
	CheckInterval *Interval `json:"checkInterval,omitempty"`
	// AccessKeyRef points to the secret with kubeconfig
	// +optional
	AccessKeyRef KubeconfigSecretSource `json:"accesskeyRef,omitempty"`
	// Namespace targets RemoteCluster namespace, will be used to check permissions and to deploy arbiter
	// +default="default"
	// +example="default"
	// +optional
	Namespace string `json:"namespace,omitempty"`
}

// RemoteClusterStatus defines the observed state of RemoteCluster.
type RemoteClusterStatus struct {
	// State represents current reconcile state
	State RemoteClusterState `json:"state,omitempty"`
	// Message provides an info about current state
	Message string `json:"message,omitempty"`
	// Conditions are showing reconcile steps and their execution results
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status
// +kubebuilder:printcolumn:name="State",type=string,JSONPath=`.status.state`,description="State"
// +kubebuilder:printcolumn:name="Message",type=string,JSONPath=`.status.message`,description="Message"
// RemoteCluster is the Schema for the remotclusters API
type RemoteCluster struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitempty,omitzero"`

	// spec defines the desired state of RemoteCluster
	// +required
	Spec RemoteClusterSpec `json:"spec"`

	// status defines the observed state of RemoteCluster
	// +optional
	Status RemoteClusterStatus `json:"status,omitempty,omitzero"`
}

// +kubebuilder:object:root=true

// RemoteClusterList contains a list of RemoteCluster
type RemoteClusterList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitempty"`
	Items           []RemoteCluster `json:"items"`
}

func init() {
	SchemeBuilder.Register(&RemoteCluster{}, &RemoteClusterList{})
}
