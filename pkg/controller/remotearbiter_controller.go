// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"

	rookv1 "github.com/rook/rook/pkg/apis/ceph.rook.io/v1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"
)

// RemoteArbiterReconciler reconciles a RemoteArbiter object
type RemoteArbiterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

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

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the RemoteArbiter object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *RemoteArbiterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = logf.FromContext(ctx)

	// TODO(user): your logic here

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RemoteArbiterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.RemoteArbiter{}).
		Named("remotearbiter").
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findProperArbiter),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&corev1.ConfigMap{},
			handler.EnqueueRequestsFromMapFunc(r.findProperArbiter),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&appsv1.Deployment{},
			handler.EnqueueRequestsFromMapFunc(r.findProperArbiter),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Watches(
			&rookv1.CephCluster{},
			handler.EnqueueRequestsFromMapFunc(r.findProperArbiter),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

func (r *RemoteArbiterReconciler) findProperArbiter(ctx context.Context, configMap client.Object) []reconcile.Request {
	return nil
}
