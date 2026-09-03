// Copyright 2026 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package v1alpha1

import (
	"context"
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/log/zap"
)

// ctx is a plain background context shared by the defaulter/validator specs.
// These are unit tests, so no cancellation or deadline is needed.
var ctx = context.Background()

// The webhook defaulter/validator specs are pure unit tests: they call
// Default/Validate directly, with no admission round-trip and no API server.
// This entrypoint therefore only wires Ginkgo and a logger; it starts no
// envtest environment and requires no KUBEBUILDER_ASSETS.
func TestWebhooks(t *testing.T) {
	RegisterFailHandler(Fail)
	logf.SetLogger(zap.New(zap.WriteTo(GinkgoWriter), zap.UseDevMode(true)))

	suiteConfig, reporterConfig := GinkgoConfiguration()
	suiteConfig.RandomizeAllSpecs = true
	RunSpecs(t, "Webhook Unit Suite", suiteConfig, reporterConfig)
}
