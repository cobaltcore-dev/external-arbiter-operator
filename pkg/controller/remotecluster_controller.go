// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"fmt"

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

	RemoteClusterTypeName = "remotecluster"

	ReasonInit  = "Init"
	ReasonOK    = "OK"
	ReasonError = "Error"
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
	log := logf.FromContext(ctx).WithValues(RemoteClusterTypeName, req.NamespacedName)

	log.Info("requesting resource")
	remoteCluster := &v1alpha1.RemoteCluster{}
	err := r.Get(ctx, req.NamespacedName, remoteCluster)
	if apierrors.IsNotFound(err) {
		log.Info("resource not found, assuming it's gone")
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
		log.Info("deletion timestamp set, cleaning up")

		if controllerutil.ContainsFinalizer(remoteCluster, RemoteClusterFinalizer) {
			secret := &corev1.Secret{}
			err := r.Get(ctx, secretObjectKey, secret)
			if apierrors.IsNotFound(err) {
				log.Info("referred resource not found, assuming it's gone", "secret", secretObjectKey)
			} else if err != nil {
				log.Error(err, "unable to get resource to remove finalizer", "secret", secretObjectKey)
				return ctrl.Result{}, err
			}

			updated := controllerutil.RemoveFinalizer(secret, RemoteClusterFinalizer)
			if updated {
				err := r.Update(ctx, remoteCluster)
				if err != nil {
					log.Error(err, "unable to update resource after finalizer removal", "secret", secretObjectKey)
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
				log.Error(err, "unable to update resource after finalizer removal")
				return ctrl.Result{}, err
			}
		} else {
			log.Info("no finalizer on resource, nothing to do")
		}

		return ctrl.Result{}, nil
	}

	if !controllerutil.ContainsFinalizer(remoteCluster, RemoteClusterFinalizer) {
		log.Info("adding finalizer on self")
		controllerutil.AddFinalizer(remoteCluster, RemoteClusterFinalizer)
		err = r.Update(ctx, remoteCluster)
		if err != nil {
			log.Error(err, "unable to update resource with finalizer")
			return ctrl.Result{}, err
		}
	} else {
		log.Info("self finalizer present")
	}

	initialConditionsCount := len(remoteCluster.Status.Conditions)
	conditionTypes := []string{v1alpha1.ConfigAvailableConditionType, v1alpha1.ConvigValidConditionType, v1alpha1.ClusterReachableConditionType, v1alpha1.HasEnoughPermissionsConditionType}

	for _, conditionType := range conditionTypes {
		existingConition := meta.FindStatusCondition(remoteCluster.Status.Conditions, conditionType)
		if existingConition == nil {
			log.Info("condition not present, initializing", "condition", conditionType)
			condition := metav1.Condition{
				Type:    conditionType,
				Status:  metav1.ConditionUnknown,
				Reason:  ReasonInit,
				Message: "init",
			}
			_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
		}
	}

	if initialConditionsCount != len(remoteCluster.Status.Conditions) {
		log.Info("updating resource with init condition")
		err = r.Status().Update(ctx, remoteCluster)
		if err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
	}

	log.Info("getting referred secret")
	secret := &corev1.Secret{}
	err = r.Get(ctx, secretObjectKey, secret)
	if err != nil {
		log.Error(err, "unable to ger referred resource", "secret", secretObjectKey)

		condition := metav1.Condition{
			Type:    v1alpha1.ConfigAvailableConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonError,
			Message: err.Error(),
		}

		if !meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
			_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
			err := r.Status().Update(ctx, remoteCluster)
			if err != nil {
				log.Error(err, "unable to update resource with conditions")
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, err
	}

	if !controllerutil.ContainsFinalizer(secret, RemoteClusterFinalizer) {
		log.Info("adding finalizer on secret")
		controllerutil.AddFinalizer(secret, RemoteClusterFinalizer)
		err = r.Update(ctx, secret)
		if err != nil {
			log.Error(err, "unable to update resource with finalizer", "secret", secretObjectKey)
			return ctrl.Result{}, err
		}
	} else {
		log.Info("secret finalizer present")
	}

	remoteKubeconfigBase64, ok := secret.Data[remoteCluster.Spec.AccessKeyRef.Key]
	if !ok {
		err := fmt.Errorf("secret key %s not found", remoteCluster.Spec.AccessKeyRef.Key)
		log.Error(err, "unable to get secret key", "secret", secretObjectKey, "key", remoteCluster.Spec.AccessKeyRef.Key)

		condition := metav1.Condition{
			Type:    v1alpha1.ConfigAvailableConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonError,
			Message: err.Error(),
		}

		if !meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
			_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
			err := r.Status().Update(ctx, remoteCluster)
			if err != nil {
				log.Error(err, "unable to update resource with conditions")
				return ctrl.Result{}, err
			}
		}
		return ctrl.Result{}, err
	} else {
		log.Info("secret key present")
	}

	condition := metav1.Condition{
		Type:    v1alpha1.ConfigAvailableConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonOK,
		Message: "config is present",
	}

	if !meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
		_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
		err = r.Status().Update(ctx, remoteCluster)
		if err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
	}

	remoteRestConfig, err := clientcmd.RESTConfigFromKubeConfig(remoteKubeconfigBase64)
	if err != nil {
		log.Error(err, "unable to build client config", "secret", secretObjectKey, "key", remoteCluster.Spec.AccessKeyRef.Key)

		condition := metav1.Condition{
			Type:    v1alpha1.ConvigValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonError,
			Message: err.Error(),
		}

		if !meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
			_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
			err := r.Status().Update(ctx, remoteCluster)
			if err != nil {
				log.Error(err, "unable to update resource with conditions")
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, err
	} else {
		log.Info("client config built")
	}

	remoteClient, err := kubernetes.NewForConfig(remoteRestConfig)
	if err != nil {
		log.Error(err, "unable to initialize client", "secret", secretObjectKey, "key", remoteCluster.Spec.AccessKeyRef.Key)

		condition := metav1.Condition{
			Type:    v1alpha1.ConvigValidConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonError,
			Message: err.Error(),
		}

		if !meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
			_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
			err := r.Status().Update(ctx, remoteCluster)
			if err != nil {
				log.Error(err, "unable to update resource with conditions")
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, err
	} else {
		log.Info("client initialized")
	}

	condition = metav1.Condition{
		Type:    v1alpha1.ConvigValidConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonOK,
		Message: "config is valid",
	}

	if !meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
		_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
		err = r.Status().Update(ctx, remoteCluster)
		if err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
	}

	readinessResponseBytes, err := remoteClient.RESTClient().Get().AbsPath("readyz").Do(ctx).Raw()
	if err != nil {
		log.Error(err, "unable to check cluster readiness")

		condition := metav1.Condition{
			Type:    v1alpha1.ClusterReachableConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonError,
			Message: err.Error(),
		}

		if !meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
			_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
			err := r.Status().Update(ctx, remoteCluster)
			if err != nil {
				log.Error(err, "unable to update resource with conditions")
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, err
	}

	readinessResponse := string(readinessResponseBytes)
	if readinessResponse != "ok" {
		err := fmt.Errorf("cluster readiness response is %s", readinessResponse)
		log.Error(err, "unable to validate cluster readiness")

		condition := metav1.Condition{
			Type:    v1alpha1.ClusterReachableConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonError,
			Message: err.Error(),
		}

		if !meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
			_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
			err := r.Status().Update(ctx, remoteCluster)
			if err != nil {
				log.Error(err, "unable to update resource with conditions")
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, err
	} else {
		log.Info("cluster is ready")
	}

	condition = metav1.Condition{
		Type:    v1alpha1.ClusterReachableConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonOK,
		Message: "cluster is ready",
	}

	if !meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
		_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
		err = r.Status().Update(ctx, remoteCluster)
		if err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
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
			log.Error(err, "unable to perform permission evaluation")

			condition := metav1.Condition{
				Type:    v1alpha1.ClusterReachableConditionType,
				Status:  metav1.ConditionFalse,
				Reason:  ReasonError,
				Message: err.Error(),
			}

			if !meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
				_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
				err := r.Status().Update(ctx, remoteCluster)
				if err != nil {
					log.Error(err, "unable to update resource with conditions")
					return ctrl.Result{}, err
				}
			}

			return ctrl.Result{}, err
		}

		if !resp.Status.Allowed {
			denyReason := resp.Status.Reason
			evalFailureReason := resp.Status.EvaluationError
			errCount := len(permissionEvalErrors)
			if len(denyReason) > 0 {
				err := errors.New(denyReason)
				permissionEvalErrors = append(permissionEvalErrors, err)
				log.Error(err, "access denied")
			}
			if len(evalFailureReason) > 0 {
				err := errors.New(evalFailureReason)
				permissionEvalErrors = append(permissionEvalErrors, err)
				log.Error(err, "evaluation failed")
			}
			if errCount == len(permissionEvalErrors) {
				err := fmt.Errorf("not enough rights for %s", permissionsReviewRequest.Spec.ResourceAttributes.String())
				permissionEvalErrors = append(permissionEvalErrors, err)
				log.Error(err, "access denied")
			}
		}
	}

	if len(permissionEvalErrors) > 0 {
		err := errors.Join(permissionEvalErrors...)
		log.Error(err, "unable to perform permission evaluation")

		condition := metav1.Condition{
			Type:    v1alpha1.HasEnoughPermissionsConditionType,
			Status:  metav1.ConditionFalse,
			Reason:  ReasonError,
			Message: err.Error(),
		}

		if !meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
			_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
			err := r.Status().Update(ctx, remoteCluster)
			if err != nil {
				log.Error(err, "unable to update resource with conditions")
				return ctrl.Result{}, err
			}
		}

		return ctrl.Result{}, err
	}

	condition = metav1.Condition{
		Type:    v1alpha1.HasEnoughPermissionsConditionType,
		Status:  metav1.ConditionTrue,
		Reason:  ReasonOK,
		Message: "user has enough permissions",
	}

	if !meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
		_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
		err = r.Status().Update(ctx, remoteCluster)
		if err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}
	}

	return ctrl.Result{RequeueAfter: remoteCluster.Spec.CheckInterval.Duration}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *RemoteClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.RemoteCluster{}).
		Named(RemoteClusterTypeName).
		Complete(r)
}
