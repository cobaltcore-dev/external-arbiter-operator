// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"

	authorizationv1 "k8s.io/api/authorization/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	logf "sigs.k8s.io/controller-runtime/pkg/log"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"
)

const (
	RemoteClusterFinalizer = "remotecluster.ceph.cobaltcore.sap.com/finalizer"
)

// RemoteArbiterReconciler reconciles a RemoteArbiter object
type RemoteClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the RemoteArbiter object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.22.1/pkg/reconcile
func (r *RemoteClusterReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	log := logf.FromContext(ctx).WithValues("remotecluster", req.NamespacedName)

	remoteCluster := &v1alpha1.RemoteCluster{}
	err := r.Get(ctx, req.NamespacedName, remoteCluster)
	if apierrors.IsNotFound(err) {
		log.Info("resource not found, probably gone")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}
	if err != nil {
		log.Error(err, "unable to get resource")
		return ctrl.Result{}, err
	}

	secretObjectKey := types.NamespacedName{
		Namespace: remoteCluster.Namespace,
		Name:      remoteCluster.Spec.AccessKeyRef.Name,
	}

	if remoteCluster.GetDeletionTimestamp() != nil {
		if controllerutil.ContainsFinalizer(remoteCluster, RemoteClusterFinalizer) {
			secret := &corev1.Secret{}
			err := r.Get(ctx, secretObjectKey, secret)
			if apierrors.IsNotFound(err) {
				log.Info("referred secret not found", "secret", secretObjectKey)
			} else if err != nil {
				log.Error(err, "unable to get resource on finalizer removal", "secret", secretObjectKey)
				return ctrl.Result{}, err
			}
			updated := controllerutil.RemoveFinalizer(secret, RemoteClusterFinalizer)
			if updated {
				err := r.Update(ctx, remoteCluster)
				if err != nil {
					log.Error(err, "unable to update resource on finalizer removal", "secret", secretObjectKey)
					return ctrl.Result{}, err
				}
			} else {
				log.Info("no finalizer on resource, nothing to do", "secret", secretObjectKey)
			}
		}

		updated := controllerutil.RemoveFinalizer(remoteCluster, RemoteClusterFinalizer)
		if updated {
			err := r.Update(ctx, remoteCluster)
			if err != nil {
				log.Error(err, "unable to update resource on finalizer removal")
				return ctrl.Result{}, err
			}
		} else {
			log.Info("no finalizer on resource, nothing to do")
		}

		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(remoteCluster, RemoteClusterFinalizer) {
		controllerutil.AddFinalizer(remoteCluster, RemoteClusterFinalizer)
		err = r.Update(ctx, remoteCluster)
		if err != nil {
			log.Error(err, "unable to update resource with finalizer")
			return ctrl.Result{}, err
		}
	}

	initialConditionsCount := len(remoteCluster.Status.Conditions)
	conditionTypes := []string{v1alpha1.ConfigAvailableConditionType, v1alpha1.ConvigValidConditionType, v1alpha1.ClusterReachableConditionType, v1alpha1.HasEnoughPermissionsConditionType}

	for _, conditionType := range conditionTypes {
		existingConition := meta.FindStatusCondition(remoteCluster.Status.Conditions, conditionType)
		if existingConition == nil {
			condition := metav1.Condition{
				Type:    conditionType,
				Status:  metav1.ConditionUnknown,
				Reason:  "Init",
				Message: "Init",
			}
			meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
		}
	}

	if initialConditionsCount != len(remoteCluster.Status.Conditions) {
		err = r.Status().Update(ctx, remoteCluster)
		if err != nil {
			log.Error(err, "unable to update resource with finalizer")
			return ctrl.Result{}, err
		}
	}

	secret := &corev1.Secret{}
	err = r.Get(ctx, secretObjectKey, secret)
	if err != nil {
		condition := metav1.Condition{
			Type:    v1alpha1.ConfigAvailableConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  "Error",
			Message: err.Error(),
		}

		if !meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
			meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
			err := r.Status().Update(ctx, remoteCluster)
			if err != nil {
				log.Error(err, "unable to update resource with finalizer")
				return ctrl.Result{}, err
			}
		}

		log.Error(err, "unable to get resource on finalizer removal", "secret", secretObjectKey)
		return ctrl.Result{}, err
	}

	remoteKubeconfigBase64, ok := secret.Data[remoteCluster.Spec.AccessKeyRef.Key]
	if !ok {
		log.Error(err, "unable to get resource on finalizer removal", "secret", secretObjectKey)
		return ctrl.Result{}, err
	}

	remoteRestConfig, err := clientcmd.RESTConfigFromKubeConfig(remoteKubeconfigBase64)
	if err != nil {
		log.Error(err, "unable to get resource on finalizer removal", "secret", secretObjectKey)
		return ctrl.Result{}, err
	}

	remoteClient, err := kubernetes.NewForConfig(remoteRestConfig)
	if err != nil {
		log.Error(err, "unable to get resource on finalizer removal", "secret", secretObjectKey)
		return ctrl.Result{}, err
	}

	readinessResponse, err := remoteClient.RESTClient().Get().AbsPath("readyz").Do(ctx).Raw()
	if err != nil {
		log.Error(err, "unable to get resource on finalizer removal", "secret", secretObjectKey)
		return ctrl.Result{}, err
	}

	if string(readinessResponse) != "ok" {
		log.Error(err, "unable to get resource on finalizer removal", "secret", secretObjectKey)
		return ctrl.Result{}, err
	}

	permissionsReviewRequests := []*authorizationv1.SelfSubjectAccessReview{
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: remoteCluster.Spec.Namespace,
					Verb:      "*",
					Resource:  "deployments",
					Group:     "apps",
					Version:   "v1",
				},
			},
		},
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace:   remoteCluster.Spec.Namespace,
					Verb:        "*",
					Resource:    "deployments",
					Group:       "apps",
					Version:     "v1",
					Subresource: "status",
				},
			},
		},
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace:   remoteCluster.Spec.Namespace,
					Verb:        "*",
					Resource:    "deployments",
					Group:       "apps",
					Version:     "v1",
					Subresource: "finalizers",
				},
			},
		},
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: remoteCluster.Spec.Namespace,
					Verb:      "*",
					Resource:  "secrets",
					Group:     "",
					Version:   "v1",
				},
			},
		},
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace:   remoteCluster.Spec.Namespace,
					Verb:        "*",
					Resource:    "secrets",
					Group:       "",
					Version:     "v1",
					Subresource: "status",
				},
			},
		},
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace:   remoteCluster.Spec.Namespace,
					Verb:        "*",
					Resource:    "secrets",
					Group:       "",
					Version:     "v1",
					Subresource: "finalizers",
				},
			},
		},
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: remoteCluster.Spec.Namespace,
					Verb:      "*",
					Resource:  "configmaps",
					Group:     "",
					Version:   "v1",
				},
			},
		},
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace:   remoteCluster.Spec.Namespace,
					Verb:        "*",
					Resource:    "configmaps",
					Group:       "",
					Version:     "v1",
					Subresource: "status",
				},
			},
		},
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace:   remoteCluster.Spec.Namespace,
					Verb:        "*",
					Resource:    "configmaps",
					Group:       "",
					Version:     "v1",
					Subresource: "finalizers",
				},
			},
		},
	}

	permissionEvalErrors := make([]error, 0)
	for _, permissionsReviewRequest := range permissionsReviewRequests {
		resp, err := remoteClient.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, permissionsReviewRequest, metav1.CreateOptions{})
		if err != nil {
			log.Error(err, "unable to perform permission evaluation", "secret", secretObjectKey)
			return ctrl.Result{}, err
		}

		if !resp.Status.Allowed {
			denyReason := resp.Status.Reason
			evalFailureReason := resp.Status.EvaluationError
			if len(denyReason) > 0 {
				err := errors.New(denyReason)
				permissionEvalErrors = append(permissionEvalErrors, err)
				log.Error(err, "unable to get resource on finalizer removal", "secret", secretObjectKey)
			}
			if len(evalFailureReason) > 0 {
				err := errors.New(evalFailureReason)
				permissionEvalErrors = append(permissionEvalErrors, err)
				log.Error(err, "unable to get resource on finalizer removal", "secret", secretObjectKey)
			}
		}
	}

	if len(permissionEvalErrors) > 0 {
		return ctrl.Result{}, errors.Join(permissionEvalErrors...)
	}

	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RemoteClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.RemoteCluster{}).
		Named("remotecluster").
		Complete(r)
}
