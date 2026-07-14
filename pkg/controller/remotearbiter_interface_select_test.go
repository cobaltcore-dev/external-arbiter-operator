// Copyright 2026 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import "testing"

func TestPreferedSourceIP(t *testing.T) {
	address, err := PreferedSourceIP("8.8.8.8:53")
	println(address.String())

	if err != nil {
		t.Errorf("unexpected error %s", err)
	}
}
