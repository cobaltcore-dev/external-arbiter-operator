// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"context"
	"errors"
	"fmt"
	"time"

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
	"sigs.k8s.io/controller-runtime/pkg/builder"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/predicate"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"
)

const (
	RemoteClusterFinalizer = "remotecluster.ceph.cobaltcore.sap.com/finalizer"

	RemoteClusterSecretKey = ".remotecluster.secret"
)

// RemoteArbiterReconciler reconciles a RemoteArbiter object
type RemoteClusterReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

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

		if remoteCluster.Status.State != v1alpha1.RemoteClusterDeletingState {
			remoteCluster.Status.State = v1alpha1.RemoteClusterDeletingState
			remoteCluster.Status.Message = "deleting"

			err := r.Status().Update(ctx, remoteCluster)
			if err != nil {
				log.Error(err, "unable to update resource status on removal")
				return ctrl.Result{}, err
			}
		}

		if controllerutil.ContainsFinalizer(remoteCluster, RemoteArbiterFinalizer) {
			log.Info("remote arbiter clean up is not complete, will retry later")
			return ctrl.Result{RequeueAfter: 5 * time.Second}, nil
		}

		if controllerutil.ContainsFinalizer(remoteCluster, RemoteClusterFinalizer) {
			secret := &corev1.Secret{}
			err := r.Get(ctx, secretObjectKey, secret)
			if apierrors.IsNotFound(err) {
				log.Info("referred resource not found, assuming it's gone", SecretTypeName, secretObjectKey)
			} else if err != nil {
				log.Error(err, "unable to get resource to remove finalizer", SecretTypeName, secretObjectKey)
				return ctrl.Result{}, err
			} else {
				if controllerutil.ContainsFinalizer(secret, RemoteClusterFinalizer) {
					updated := controllerutil.RemoveFinalizer(secret, RemoteClusterFinalizer)
					if updated {
						err := r.Update(ctx, secret)
						if err != nil {
							log.Error(err, "unable to update resource after finalizer removal", SecretTypeName, secretObjectKey)
							return ctrl.Result{}, err
						}
					}
				}
			}

			log.Info("no finalizer on resource, proceeding", SecretTypeName, secretObjectKey)

			updated := controllerutil.RemoveFinalizer(remoteCluster, RemoteClusterFinalizer)
			if updated {
				err = r.Update(ctx, remoteCluster)
				if err != nil {
					log.Error(err, "unable to update resource after finalizer removal")
					return ctrl.Result{}, err
				}
			}
		}

		log.Info("no finalizer on resource, proceeding")

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
	}

	log.Info("self finalizer present")

	initialConditionsCount := len(remoteCluster.Status.Conditions)
	conditionTypes := []string{
		v1alpha1.ConfigAvailableConditionType,
		v1alpha1.ConfigValidConditionType,
		v1alpha1.ClusterReachableConditionType,
		v1alpha1.HasEnoughPermissionsConditionType,
	}

	for _, conditionType := range conditionTypes {
		existingConition := meta.FindStatusCondition(remoteCluster.Status.Conditions, conditionType)
		if existingConition == nil {
			log.Info("condition not present, initializing", "condition", conditionType)
			condition := NewInitCondition(conditionType, "init")
			_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)
		}
	}

	if initialConditionsCount != len(remoteCluster.Status.Conditions) {
		remoteCluster.Status.State = v1alpha1.RemoteClusterInitState
		remoteCluster.Status.Message = "initialized"

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
		log.Error(err, "unable to get referred resource", SecretTypeName, secretObjectKey)

		if err := r.updateRemoteClusterStatusOnFailure(ctx, remoteCluster,
			v1alpha1.ConfigAvailableConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, err
	}

	if !controllerutil.ContainsFinalizer(secret, RemoteClusterFinalizer) {
		log.Info("adding finalizer on secret")
		controllerutil.AddFinalizer(secret, RemoteClusterFinalizer)
		err = r.Update(ctx, secret)
		if err != nil {
			log.Error(err, "unable to update resource with finalizer", SecretTypeName, secretObjectKey)
			return ctrl.Result{}, err
		}
	}

	log.Info("secret finalizer present")

	remoteKubeconfigBase64, ok := secret.Data[remoteCluster.Spec.AccessKeyRef.Key]
	if !ok {
		err := fmt.Errorf("secret key %s not found", remoteCluster.Spec.AccessKeyRef.Key)
		log.Error(err, "unable to get secret key", SecretTypeName, secretObjectKey, "key", remoteCluster.Spec.AccessKeyRef.Key)

		if err := r.updateRemoteClusterStatusOnFailure(ctx, remoteCluster,
			v1alpha1.ConfigAvailableConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, err
	}

	statusMessage := "config is present"
	log.Info(statusMessage)

	if err := r.updateRemoteClusterStatusOnSuccess(ctx, remoteCluster,
		v1alpha1.RemoteClusterProgressingState, v1alpha1.ConfigAvailableConditionType, statusMessage); err != nil {
		log.Error(err, "unable to update resource with conditions")
		return ctrl.Result{}, err
	}

	remoteRestConfig, err := clientcmd.RESTConfigFromKubeConfig(remoteKubeconfigBase64)
	if err != nil {
		log.Error(err, "unable to build client config", SecretTypeName, secretObjectKey, "key", remoteCluster.Spec.AccessKeyRef.Key)

		if err := r.updateRemoteClusterStatusOnFailure(ctx, remoteCluster,
			v1alpha1.ConfigValidConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, err
	}

	log.Info("client config built")

	remoteClient, err := kubernetes.NewForConfig(remoteRestConfig)
	if err != nil {
		log.Error(err, "unable to initialize client", SecretTypeName, secretObjectKey, "key", remoteCluster.Spec.AccessKeyRef.Key)

		if err := r.updateRemoteClusterStatusOnFailure(ctx, remoteCluster,
			v1alpha1.ConfigValidConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, err
	}

	statusMessage = "client initialized, config is valid"
	log.Info(statusMessage)

	if err := r.updateRemoteClusterStatusOnSuccess(ctx, remoteCluster,
		v1alpha1.RemoteClusterProgressingState, v1alpha1.ConfigValidConditionType, statusMessage); err != nil {
		log.Error(err, "unable to update resource with conditions")
		return ctrl.Result{}, err
	}

	readinessResponseBytes, err := remoteClient.RESTClient().Get().AbsPath("readyz").Do(ctx).Raw()
	if err != nil {
		log.Error(err, "unable to check cluster readiness")

		if err := r.updateRemoteClusterStatusOnFailure(ctx, remoteCluster,
			v1alpha1.ClusterReachableConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, err
	}

	readinessResponse := string(readinessResponseBytes)
	if readinessResponse != "ok" {
		err := fmt.Errorf("cluster readiness response is %s", readinessResponse)
		log.Error(err, "unable to validate cluster readiness")

		if err := r.updateRemoteClusterStatusOnFailure(ctx, remoteCluster,
			v1alpha1.ClusterReachableConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, err
	}

	statusMessage = "cluster is ready"
	log.Info(statusMessage)

	if err := r.updateRemoteClusterStatusOnSuccess(ctx, remoteCluster,
		v1alpha1.RemoteClusterProgressingState, v1alpha1.ClusterReachableConditionType, statusMessage); err != nil {
		log.Error(err, "unable to update resource with conditions")
		return ctrl.Result{}, err
	}

	permissionsReviewRequests := r.getPermissionReviewRequests(remoteCluster.Spec.Namespace)

	permissionEvalErrors := make([]error, 0)
	for _, permissionsReviewRequest := range permissionsReviewRequests {
		resp, err := remoteClient.AuthorizationV1().SelfSubjectAccessReviews().Create(ctx, permissionsReviewRequest, metav1.CreateOptions{})
		if err != nil {
			log.Error(err, "unable to perform permission evaluation")

			if err := r.updateRemoteClusterStatusOnFailure(ctx, remoteCluster,
				v1alpha1.ClusterReachableConditionType, err); err != nil {
				log.Error(err, "unable to update resource with conditions")
				return ctrl.Result{}, err
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

		if err := r.updateRemoteClusterStatusOnFailure(ctx, remoteCluster,
			v1alpha1.HasEnoughPermissionsConditionType, err); err != nil {
			log.Error(err, "unable to update resource with conditions")
			return ctrl.Result{}, err
		}

		return ctrl.Result{}, err
	}

	statusMessage = "cluster is ready and configured"
	log.Info(statusMessage)

	if err := r.updateRemoteClusterStatusOnSuccess(ctx, remoteCluster,
		v1alpha1.RemoteClusterReadyState, v1alpha1.HasEnoughPermissionsConditionType, statusMessage); err != nil {
		log.Error(err, "unable to update resource with conditions")
		return ctrl.Result{}, err
	}

	return ctrl.Result{RequeueAfter: remoteCluster.Spec.CheckInterval.Duration}, nil
}

// func (r *RemoteClusterReconciler) updateRemoteClusterCondition(ctx context.Context, log logr.Logger, remoteCluster *v1alpha1.RemoteCluster, condition metav1.Condition) error {
// 	if changed := meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition); !changed {
// 		log.Info("condition hasn't changed, nothing to update")
// 		return nil
// 	}

// 	if err := r.Status().Update(ctx, remoteCluster); err != nil {
// 		log.Error(err, "failed to update resource status")
// 		return err
// 	}

// 	return nil
// }

func (r *RemoteClusterReconciler) updateRemoteClusterStatusOnFailure(
	ctx context.Context, remoteCluster *v1alpha1.RemoteCluster, conditionType string, err error) error {
	statusMessage := err.Error()
	stateSet := r.setRemoteClusterState(remoteCluster, v1alpha1.RemoteClusterErrorState, statusMessage)
	condition := NewErrorCondition(conditionType, statusMessage)
	conditionSet := r.setRemoteClusterCondition(remoteCluster, condition)
	if !stateSet && !conditionSet {
		return nil
	}
	if err := r.Status().Update(ctx, remoteCluster); err != nil {
		return err
	}

	return nil
}

func (r *RemoteClusterReconciler) updateRemoteClusterStatusOnSuccess(
	ctx context.Context, remoteCluster *v1alpha1.RemoteCluster, state v1alpha1.RemoteClusterState, conditionType string, statusMessage string) error {
	_ = r.setRemoteClusterState(remoteCluster, state, statusMessage)
	condition := NewOKCondition(conditionType, statusMessage)
	conditionSet := r.setRemoteClusterCondition(remoteCluster, condition)
	if !conditionSet {
		return nil
	}
	if err := r.Status().Update(ctx, remoteCluster); err != nil {
		return err
	}

	return nil
}

func (r *RemoteClusterReconciler) setRemoteClusterState(remoteCluster *v1alpha1.RemoteCluster, state v1alpha1.RemoteClusterState, message string) bool {
	if remoteCluster.Status.State == state && remoteCluster.Status.Message == message {
		return false
	}

	remoteCluster.Status.State = state
	remoteCluster.Status.Message = message

	return true
}

func (r *RemoteClusterReconciler) setRemoteClusterCondition(remoteCluster *v1alpha1.RemoteCluster, condition metav1.Condition) bool {
	if meta.IsStatusConditionPresentAndEqual(remoteCluster.Status.Conditions, condition.Type, condition.Status) {
		return false
	}

	_ = meta.SetStatusCondition(&remoteCluster.Status.Conditions, condition)

	return true
}

func (r *RemoteClusterReconciler) getPermissionReviewRequests(namespace string) []*authorizationv1.SelfSubjectAccessReview {
	verbs := []string{"create", "delete", "get", "list", "patch", "update", "watch"}
	reviewRequestTemplates := []*authorizationv1.SelfSubjectAccessReview{
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: namespace,
					Resource:  "deployments",
					Group:     "apps",
					Version:   "v1",
				},
			},
		},
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: namespace,
					Resource:  "secrets",
					Group:     "",
					Version:   "v1",
				},
			},
		},
		{
			Spec: authorizationv1.SelfSubjectAccessReviewSpec{
				ResourceAttributes: &authorizationv1.ResourceAttributes{
					Namespace: namespace,
					Resource:  "configmaps",
					Group:     "",
					Version:   "v1",
				},
			},
		},
	}

	reviewRequests := make([]*authorizationv1.SelfSubjectAccessReview, 0, len(reviewRequestTemplates)*(2*len(verbs)+1))
	for _, reviewRequestTemplate := range reviewRequestTemplates {
		for _, verb := range verbs {
			resourceReviewRequest := reviewRequestTemplate.DeepCopy()
			resourceReviewRequest.Spec.ResourceAttributes.Verb = verb

			statusReviewRequest := reviewRequestTemplate.DeepCopy()
			statusReviewRequest.Spec.ResourceAttributes.Verb = verb
			statusReviewRequest.Spec.ResourceAttributes.Subresource = "status"

			reviewRequests = append(reviewRequests, resourceReviewRequest, statusReviewRequest)
		}

		finalizerReviewRequest := reviewRequestTemplate.DeepCopy()
		finalizerReviewRequest.Spec.ResourceAttributes.Verb = "update"
		finalizerReviewRequest.Spec.ResourceAttributes.Subresource = "finalizers"
		reviewRequests = append(reviewRequests, finalizerReviewRequest)
	}

	return reviewRequests
}

// SetupWithManager sets up the controller with the Manager.
func (r *RemoteClusterReconciler) SetupWithManager(mgr ctrl.Manager) error {
	if err := mgr.GetFieldIndexer().IndexField(context.Background(), &v1alpha1.RemoteCluster{}, RemoteClusterSecretKey, func(rawObj client.Object) []string {
		remoteCluster := rawObj.(*v1alpha1.RemoteCluster)
		secretName := remoteCluster.Spec.AccessKeyRef.Name
		if secretName == "" {
			return nil
		}
		return []string{secretName}
	}); err != nil {
		return err
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&v1alpha1.RemoteCluster{}).
		Named(RemoteClusterTypeName).
		Watches(
			&corev1.Secret{},
			handler.EnqueueRequestsFromMapFunc(r.findClusterForSecret),
			builder.WithPredicates(predicate.ResourceVersionChangedPredicate{}),
		).
		Complete(r)
}

func (r *RemoteClusterReconciler) findClusterForSecret(ctx context.Context, object client.Object) []reconcile.Request {
	secret, ok := object.(*corev1.Secret)
	if !ok {
		return nil
	}

	remoteClusterList := &v1alpha1.RemoteClusterList{}
	if err := r.List(ctx, remoteClusterList, client.InNamespace(secret.Namespace), client.MatchingFields{RemoteClusterSecretKey: secret.Name}); err != nil {
		return nil
	}

	itemCount := len(remoteClusterList.Items)
	if itemCount == 0 {
		return nil
	}

	requests := make([]reconcile.Request, 0, itemCount)
	for _, remoteCluster := range remoteClusterList.Items {
		requests = append(requests, reconcile.Request{
			NamespacedName: client.ObjectKeyFromObject(&remoteCluster),
		})
	}

	return requests
}
