// Copyright 2026 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"testing"

	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
)

func TestDeploymentPublicAddress(t *testing.T) {
	deploymentWithArgs := func(args ...string) *appsv1.Deployment {
		return &appsv1.Deployment{
			Spec: appsv1.DeploymentSpec{
				Template: corev1.PodTemplateSpec{
					Spec: corev1.PodSpec{
						Containers: []corev1.Container{
							{
								Name: "mon",
								Args: args,
							},
						},
					},
				},
			},
		}
	}

	tests := []struct {
		name       string
		deployment *appsv1.Deployment
		want       string
	}{
		{
			name:       "extracts the service cluster ip",
			deployment: deploymentWithArgs("--fsid=abc", "--public-addr=10.96.147.96", "--id=ext-a"),
			want:       "10.96.147.96",
		},
		{
			name:       "extracts the load balancer ingress ip",
			deployment: deploymentWithArgs("--fsid=abc", "--public-addr=192.0.2.10"),
			want:       "192.0.2.10",
		},
		{
			name:       "extracts the legacy pod ip placeholder",
			deployment: deploymentWithArgs("--fsid=abc", "--public-addr=$(ROOK_POD_IP)"),
			want:       "$(ROOK_POD_IP)",
		},
		{
			name:       "returns empty when no public address argument is set",
			deployment: deploymentWithArgs("--fsid=abc", "--id=ext-a"),
			want:       "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := deploymentPublicAddress(tt.deployment)
			if got != tt.want {
				t.Errorf("deploymentPublicAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}
