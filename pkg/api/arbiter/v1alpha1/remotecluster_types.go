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
	ConvigValidConditionType          = "ConfigValid"
	ClusterReachableConditionType     = "ClusterReachable"
	HasEnoughPermissionsConditionType = "HasEnoughPermissions"
)

type KubeconfigSecretSource struct {
	// +required
	Name string `json:"name,omitempty"`
	// +required
	Key string `json:"key,omitempty"`
}

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
	// +default="default"
	// +example="default"
	// +optional
	Namespace string `json:"namespace,omitempty"`
	// +required
	AccessKeyRef KubeconfigSecretSource `json:"accesskeyRef,omitempty"`
	// +default="1m"
	// +example="1m"
	// +optional
	CheckInterval Interval `json:"checkInterval,omitempty"`
}

// RemoteClusterStatus defines the observed state of RemoteCluster.
type RemoteClusterStatus struct {
	State   string `json:"state,omitempty"`
	Message string `json:"message,omitempty"`
	// +listType=map
	// +listMapKey=type
	// +optional
	Conditions []metav1.Condition `json:"conditions,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

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
