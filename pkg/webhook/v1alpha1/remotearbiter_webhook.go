// Copyright 2026 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"context"
	"net/netip"
	"time"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/validation"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/util/validation/field"
	ctrl "sigs.k8s.io/controller-runtime"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"
)

const (
	DefaultMonIDPrefix = "ext-"
)

var remotearbiterlog = logf.Log.WithName("remotearbiter-resource")

func SetupRemoteArbiterWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &v1alpha1.RemoteArbiter{}).
		WithValidator(&RemoteArbiterCustomValidator{}).
		WithDefaulter(&RemoteArbiterCustomDefaulter{}).
		Complete()
}

// +kubebuilder:webhook:path=/mutate-ceph-cobaltcore-sap-com-v1alpha1-remotearbiter,mutating=true,failurePolicy=fail,sideEffects=None,groups=ceph.cobaltcore.sap.com,resources=remotearbiters,verbs=create;update,versions=v1alpha1,name=mremotearbiter-v1alpha1.kb.io,admissionReviewVersions=v1

type RemoteArbiterCustomDefaulter struct{}

// Default implements webhook.CustomDefaulter so a webhook will be registered for the Kind RemoteArbiter.
func (r *RemoteArbiterCustomDefaulter) Default(_ context.Context, remoteArbiter *v1alpha1.RemoteArbiter) error {
	remotearbiterlog.Info("Defaulting for RemoteArbiter", "name", remoteArbiter.GetName())

	if remoteArbiter.Spec.CheckInterval == nil {
		remoteArbiter.Spec.CheckInterval = &v1alpha1.Interval{
			Duration: time.Minute,
		}
	}
	if remoteArbiter.Spec.CephCluster.Namespace == "" {
		remoteArbiter.Spec.CephCluster.Namespace = remoteArbiter.Namespace
	}
	if remoteArbiter.Spec.MonIDPrefix == "" {
		remoteArbiter.Spec.MonIDPrefix = DefaultMonIDPrefix
	}

	if remoteArbiter.Spec.RemoteCluster.Name == "" && remoteArbiter.Spec.RemoteCluster.Spec != nil {
		setRemoteClusterSpecDefaults(remoteArbiter.Spec.RemoteCluster.Spec, remoteArbiter.Name)
	}

	return nil
}

// +kubebuilder:webhook:path=/validate-ceph-cobaltcore-sap-com-v1alpha1-remotearbiter,mutating=false,failurePolicy=fail,sideEffects=None,groups=ceph.cobaltcore.sap.com,resources=remotearbiters,verbs=create;update,versions=v1alpha1,name=vremotearbiter-v1alpha1.kb.io,admissionReviewVersions=v1

type RemoteArbiterCustomValidator struct{}

// ValidateCreate implements webhook.CustomValidator so a webhook will be registered for the type RemoteArbiter.
func (r *RemoteArbiterCustomValidator) ValidateCreate(ctx context.Context, remoteArbiter *v1alpha1.RemoteArbiter) (warnings admission.Warnings, err error) {
	remotearbiterlog.Info("Validation for RemoteArbiter upon creation", "name", remoteArbiter.GetName())
	return r.validate(remoteArbiter)
}

// ValidateUpdate implements webhook.CustomValidator so a webhook will be registered for the type RemoteArbiter.
func (r *RemoteArbiterCustomValidator) ValidateUpdate(ctx context.Context, oldObj, remoteArbiter *v1alpha1.RemoteArbiter) (warnings admission.Warnings, err error) {
	remotearbiterlog.Info("Validation for RemoteArbiter upon update", "name", remoteArbiter.GetName())
	return r.validate(remoteArbiter)
}

// ValidateDelete implements webhook.CustomValidator so a webhook will be registered for the type RemoteArbiter.
func (r *RemoteArbiterCustomValidator) ValidateDelete(ctx context.Context, remoteArbiter *v1alpha1.RemoteArbiter) (warnings admission.Warnings, err error) {
	remotearbiterlog.Info("Validation for RemoteArbiter upon deletion", "name", remoteArbiter.GetName())
	return r.validate(remoteArbiter)
}

func (r *RemoteArbiterCustomValidator) validate(remoteArbiter *v1alpha1.RemoteArbiter) (admission.Warnings, error) {
	rootPath := field.NewPath("spec")
	validationErrors := validateRemoteArbiterSpec(&remoteArbiter.Spec, rootPath)

	if len(validationErrors) > 0 {
		gvk := remoteArbiter.GroupVersionKind()
		gk := schema.GroupKind{
			Group: gvk.Group,
			Kind:  gvk.Kind,
		}
		return nil, apierrors.NewInvalid(gk, remoteArbiter.Name, validationErrors)
	}

	return nil, nil
}

func validateRemoteArbiterSpec(remoteArbiterSpec *v1alpha1.RemoteArbiterSpec, rootPath *field.Path) field.ErrorList {
	var validationErrors field.ErrorList

	if remoteArbiterSpec.CheckInterval == nil {
		validationErrors = append(validationErrors, field.Invalid(
			rootPath.Child("checkInterval"), remoteArbiterSpec.CheckInterval, "check interval should not be empty"))
	} else if remoteArbiterSpec.CheckInterval.Duration < 0 {
		validationErrors = append(validationErrors, field.Invalid(
			rootPath.Child("checkInterval"), remoteArbiterSpec.CheckInterval, "check interval should not be negative"))
	}

	errMsgs := validation.NameIsDNSLabel(remoteArbiterSpec.CephCluster.Name, false)
	if len(errMsgs) != 0 {
		for _, errMsg := range errMsgs {
			validationErrors = append(validationErrors, field.Invalid(
				rootPath.Child("cephCluster", "name"), remoteArbiterSpec.CephCluster.Name, errMsg))
		}
	}

	errMsgs = validation.NameIsDNSLabel(remoteArbiterSpec.CephCluster.Namespace, false)
	if len(errMsgs) != 0 {
		for _, errMsg := range errMsgs {
			validationErrors = append(validationErrors, field.Invalid(
				rootPath.Child("cephCluster", "namespace"), remoteArbiterSpec.CephCluster.Name, errMsg))
		}
	}

	errMsgs = validation.NameIsDNSLabel(remoteArbiterSpec.MonIDPrefix, true)
	if len(errMsgs) != 0 {
		for _, errMsg := range errMsgs {
			validationErrors = append(validationErrors, field.Invalid(
				rootPath.Child("monIdPrefix"), remoteArbiterSpec.RemoteCluster.Name, errMsg))
		}
	}

	if remoteArbiterSpec.RemoteCluster.Name == "" && remoteArbiterSpec.RemoteCluster.Spec == nil {
		validationErrors = append(validationErrors, field.Invalid(
			rootPath.Child("remoteCluster", "name"), remoteArbiterSpec.RemoteCluster.Name, "should provide only one of: remote cluster name or spec"))
		validationErrors = append(validationErrors, field.Invalid(
			rootPath.Child("remoteCluster", "spec"), remoteArbiterSpec.RemoteCluster.Name, "should provide only one of: remote cluster name or spec"))
	}

	if remoteArbiterSpec.RemoteCluster.Name != "" {
		errMsgs := validation.NameIsDNSLabel(remoteArbiterSpec.RemoteCluster.Name, false)
		if len(errMsgs) != 0 {
			for _, errMsg := range errMsgs {
				validationErrors = append(validationErrors, field.Invalid(
					rootPath.Child("remoteCluster", "name"), remoteArbiterSpec.RemoteCluster.Name, errMsg))
			}
		}
	}

	if remoteArbiterSpec.RemoteCluster.Spec != nil {
		remoteClusterSpecValidationErrors := validateRemoteClusterSpec(remoteArbiterSpec.RemoteCluster.Spec, rootPath.Child("remoteCluster", "spec"))
		if len(remoteClusterSpecValidationErrors) > 0 {
			validationErrors = append(validationErrors, remoteClusterSpecValidationErrors...)
		}
	}

	if remoteArbiterSpec.Service != nil {
		if len(remoteArbiterSpec.Service.Type) == 0 {
			validationErrors = append(validationErrors, field.Invalid(
				rootPath.Child("service", "type"), remoteArbiterSpec.Service.Type, "service type should not be empty"))
		}

		if remoteArbiterSpec.Service.Type == corev1.ServiceTypeNodePort {
			parsedIP, err := netip.ParseAddr(remoteArbiterSpec.Service.NodeIP)
			if err != nil {
				validationErrors = append(validationErrors, field.Invalid(
					rootPath.Child("service", "nodeIp"), remoteArbiterSpec.Service.Type, err.Error()))
			}

			if !parsedIP.Is4() {
				validationErrors = append(validationErrors, field.Invalid(
					rootPath.Child("service", "nodeIp"), remoteArbiterSpec.Service.Type, "should be IPv4 address"))
			}
		}
	}

	return validationErrors
}
