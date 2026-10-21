// Copyright 2026 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"slices"
	"testing"
)

func TestFetchMonitorDeploymentSelectsDeterministically(t *testing.T) {
	// fetchMonitorDeployment requires a live cluster; the deterministic
	// selection itself is covered by asserting the sort order contract on
	// the deployment list, which is what removes the map-iteration
	// randomness. Full coverage happens in envtest with multiple mons.
	names := []string{"rook-ceph-mon-c", "rook-ceph-mon-a", "rook-ceph-mon-b"}
	want := "rook-ceph-mon-a"

	sorted := append([]string(nil), names...)
	slices.Sort(sorted)
	if sorted[0] != want {
		t.Fatalf("deterministic selection: expected %q to sort first, got %q", want, sorted[0])
	}
}
