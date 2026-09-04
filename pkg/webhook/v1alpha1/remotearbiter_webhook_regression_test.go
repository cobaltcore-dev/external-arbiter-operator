// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/validation/field"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"
)

// Regression guard: validateRemoteArbiterSpec builds field.Invalid errors, and
// the BadValue must be the offending field's OWN value. Two branches previously
// reported the wrong field's value (cephCluster.namespace reported .Name;
// monIdPrefix reported remoteCluster.Name), which produced misleading admission
// errors. The existing suite only asserted "validation failed", so it never
// caught this. Assert the reported value matches the field it names.
func TestValidateRemoteArbiterSpecErrorBadValue(t *testing.T) {
	spec := &v1alpha1.RemoteArbiterSpec{
		CheckInterval: &v1alpha1.Interval{Duration: 0},
		CephCluster: v1alpha1.NamespacedReference{
			Name:      "valid-name",
			Namespace: ".invalid-namespace",
		},
		RemoteCluster: v1alpha1.RemoteClusterConfiguration{Name: "valid-remote"},
		MonIDPrefix:   ".invalid-prefix",
	}

	errs := validateRemoteArbiterSpec(spec, field.NewPath("spec"))

	want := map[string]string{
		"spec.cephCluster.namespace": spec.CephCluster.Namespace,
		"spec.monIdPrefix":           spec.MonIDPrefix,
	}
	got := map[string]string{}
	for _, e := range errs {
		if _, ok := want[e.Field]; ok {
			bad, _ := e.BadValue.(string)
			got[e.Field] = bad
		}
	}

	for fieldName, wantVal := range want {
		gotVal, ok := got[fieldName]
		if !ok {
			t.Errorf("expected a validation error for %s, got none", fieldName)
			continue
		}
		if gotVal != wantVal {
			t.Errorf("%s: error BadValue = %q, want the field's own value %q", fieldName, gotVal, wantVal)
		}
	}
}

// Regression guard for the NodePort nodeIp branch: it reported Service.Type as
// the BadValue (same field-value bug class as above), and on an unparseable IP
// it fell through to the Is4() check and appended a SECOND spurious error. A bad
// NodeIP must yield exactly one error whose BadValue is the NodeIP itself.
func TestValidateRemoteArbiterSpecNodeIPErrorBadValue(t *testing.T) {
	spec := &v1alpha1.RemoteArbiterSpec{
		CheckInterval: &v1alpha1.Interval{Duration: 1},
		CephCluster:   v1alpha1.NamespacedReference{Name: "valid-name", Namespace: "valid-ns"},
		RemoteCluster: v1alpha1.RemoteClusterConfiguration{Name: "valid-remote"},
		MonIDPrefix:   "ext-",
		Service:       &v1alpha1.ServiceConfiguration{Type: corev1.ServiceTypeNodePort, NodeIP: "not-an-ip"},
	}

	errs := validateRemoteArbiterSpec(spec, field.NewPath("spec"))

	var nodeIPErrs []*field.Error
	for _, e := range errs {
		if e.Field == "spec.service.nodeIp" {
			nodeIPErrs = append(nodeIPErrs, e)
		}
	}

	if len(nodeIPErrs) != 1 {
		t.Fatalf("expected exactly 1 nodeIp error, got %d: %v", len(nodeIPErrs), nodeIPErrs)
	}
	if bad, _ := nodeIPErrs[0].BadValue.(string); bad != spec.Service.NodeIP {
		t.Errorf("nodeIp error BadValue = %q, want the field's own value %q", bad, spec.Service.NodeIP)
	}
}
