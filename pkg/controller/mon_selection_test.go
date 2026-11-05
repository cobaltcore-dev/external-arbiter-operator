// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	appsv1 "k8s.io/api/apps/v1"
)

func TestSelectTemplateMonDeployment(t *testing.T) {
	deployments := func(names ...string) []appsv1.Deployment {
		items := make([]appsv1.Deployment, 0, len(names))
		for _, name := range names {
			items = append(items, appsv1.Deployment{ObjectMeta: metav1.ObjectMeta{Name: name}})
		}
		return items
	}

	t.Run("selects the lexicographically first monitor", func(t *testing.T) {
		got := selectTemplateMonDeployment(deployments("rook-ceph-mon-b", "rook-ceph-mon-c", "rook-ceph-mon-a"))
		if got.Name != "rook-ceph-mon-a" {
			t.Fatalf("expected template monitor %q, got %q", "rook-ceph-mon-a", got.Name)
		}
	})

	t.Run("selects the same monitor for every cache ordering", func(t *testing.T) {
		// The controller-runtime cache returns list items in Go map
		// iteration order, re-rolled on every call. Every ordering of the
		// monitor set must select the same template monitor, or each
		// reconcile may clone a different one.
		names := []string{"rook-ceph-mon-b", "rook-ceph-mon-d", "rook-ceph-mon-a", "rook-ceph-mon-c"}
		for _, ordering := range permutations(names) {
			got := selectTemplateMonDeployment(deployments(ordering...))
			if got.Name != "rook-ceph-mon-a" {
				t.Fatalf("ordering %v: expected template monitor %q, got %q", ordering, "rook-ceph-mon-a", got.Name)
			}
		}
	})

	t.Run("single monitor is selected as is", func(t *testing.T) {
		got := selectTemplateMonDeployment(deployments("rook-ceph-mon-a"))
		if got.Name != "rook-ceph-mon-a" {
			t.Fatalf("expected template monitor %q, got %q", "rook-ceph-mon-a", got.Name)
		}
	})
}

// permutations returns all orderings of values.
func permutations(values []string) [][]string {
	if len(values) <= 1 {
		return [][]string{append([]string(nil), values...)}
	}

	var result [][]string
	for i, value := range values {
		rest := append([]string(nil), values[:i]...)
		rest = append(rest, values[i+1:]...)
		for _, perm := range permutations(rest) {
			result = append(result, append([]string{value}, perm...))
		}
	}
	return result
}
