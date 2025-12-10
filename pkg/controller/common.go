// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	SecretTypeName        = "secret"
	ConfigMapTypeName     = "configmap"
	DeploymentTypeName    = "deployment"
	RemoteClusterTypeName = "remotecluster"
	RemoteArbiterTypeName = "remotearbiter"

	ReasonInit  = "Init"
	ReasonOK    = "OK"
	ReasonError = "Error"
)

// +kubebuilder:rbac:groups=core,resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=configmaps/finalizers,verbs=update
// +kubebuilder:rbac:groups=core,resources=secrets,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=core,resources=secrets/finalizers,verbs=update
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephclusters/status,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ceph.rook.io,resources=cephclusters/finalizers,verbs=update
// +kubebuilder:rbac:groups=ceph.cobaltcore.sap.com,resources=remotearbiters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ceph.cobaltcore.sap.com,resources=remotearbiters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ceph.cobaltcore.sap.com,resources=remotearbiters/finalizers,verbs=update
// +kubebuilder:rbac:groups=ceph.cobaltcore.sap.com,resources=remoteclusters,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=ceph.cobaltcore.sap.com,resources=remoteclusters/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=ceph.cobaltcore.sap.com,resources=remoteclusters/finalizers,verbs=update

func ObjectKeyFromObject(obj client.Object) *types.NamespacedName {
	return &types.NamespacedName{
		Namespace: obj.GetNamespace(),
		Name:      obj.GetName(),
	}
}

func NewInitCondition(aType string, msg string) metav1.Condition {
	return metav1.Condition{
		Type:    aType,
		Status:  metav1.ConditionUnknown,
		Reason:  ReasonInit,
		Message: msg,
	}
}

func NewOKCondition(aType string, msg string) metav1.Condition {
	return metav1.Condition{
		Type:    aType,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonOK,
		Message: msg,
	}
}

func NewErrorCondition(aType string, msg string) metav1.Condition {
	return metav1.Condition{
		Type:    aType,
		Status:  metav1.ConditionFalse,
		Reason:  ReasonError,
		Message: msg,
	}
}
