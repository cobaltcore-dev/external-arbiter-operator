// Copyright 2026 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"errors"
	"testing"

	"github.com/cobaltcore-dev/external-arbiter-operator/pkg/api/arbiter/v1alpha1"

	corev1 "k8s.io/api/core/v1"
)

func TestDeterminePublicAddress(t *testing.T) {
	remoteArbiter := &v1alpha1.RemoteArbiter{
		Spec: v1alpha1.RemoteArbiterSpec{
			Service: &v1alpha1.ServiceConfiguration{
				Type:   corev1.ServiceTypeNodePort,
				NodeIP: "192.0.2.10",
			},
		},
	}

	service := func(serviceType corev1.ServiceType, clusterIP string, ingressIPs ...string) *corev1.Service {
		ingresses := make([]corev1.LoadBalancerIngress, 0, len(ingressIPs))
		for _, ip := range ingressIPs {
			ingresses = append(ingresses, corev1.LoadBalancerIngress{IP: ip})
		}
		return &corev1.Service{
			Spec: corev1.ServiceSpec{
				Type:       serviceType,
				ClusterIP:  clusterIP,
				Selector:   map[string]string{},
				Ports:      []corev1.ServicePort{},
				IPFamilies: []corev1.IPFamily{},
			},
			Status: corev1.ServiceStatus{
				LoadBalancer: corev1.LoadBalancerStatus{
					Ingress: ingresses,
				},
			},
		}
	}

	tests := []struct {
		name    string
		service *corev1.Service
		want    string
		wantErr error
	}{
		{
			name:    "without a service the pod ip placeholder is used",
			service: nil,
			want:    "$(ROOK_POD_IP)",
		},
		{
			name:    "cluster ip service returns its cluster ip",
			service: service(corev1.ServiceTypeClusterIP, "10.96.147.96"),
			want:    "10.96.147.96",
		},
		{
			name:    "cluster ip service without an allocated cluster ip errors",
			service: service(corev1.ServiceTypeClusterIP, ""),
			wantErr: errors.New("service cluster ip is not yet allocated"),
		},
		{
			name:    "node port service returns the node ip from the spec",
			service: service(corev1.ServiceTypeNodePort, ""),
			want:    "192.0.2.10",
		},
		{
			name:    "load balancer service without ingress reports the sentinel error",
			service: service(corev1.ServiceTypeLoadBalancer, ""),
			wantErr: errLoadBalancerAddressNotAllocated,
		},
		{
			name:    "load balancer service returns the first IPv4 ingress address",
			service: service(corev1.ServiceTypeLoadBalancer, "", "2001:db8::1", "192.0.2.20"),
			want:    "192.0.2.20",
		},
		{
			name:    "load balancer service with only IPv6 ingress addresses errors",
			service: service(corev1.ServiceTypeLoadBalancer, "", "2001:db8::1"),
			wantErr: errors.New("load balancer ingress has no IPv4 address"),
		},
		{
			name:    "unsupported service types error",
			service: service(corev1.ServiceTypeExternalName, ""),
			wantErr: errors.New("unsupported service type ExternalName"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := &RemoteArbiterReconcilationState{
				remoteArbiter:  remoteArbiter,
				arbiterService: tt.service,
			}
			got, err := (&RemoteArbiterReconciler{}).determinePublicAddress(s)

			if tt.wantErr != nil {
				if err == nil {
					t.Fatalf("determinePublicAddress() = %q, want error %q", got, tt.wantErr)
				}
				if tt.wantErr.Error() != err.Error() {
					t.Errorf("determinePublicAddress() error = %q, want %q", err, tt.wantErr)
				}
				// The load balancer sentinel must stay recognizable via
				// errors.Is, the drift check relies on it
				if !errors.Is(err, errLoadBalancerAddressNotAllocated) &&
					tt.wantErr == errLoadBalancerAddressNotAllocated {
					t.Errorf("determinePublicAddress() error = %q, want it to match the load balancer sentinel via errors.Is", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("determinePublicAddress() returned unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("determinePublicAddress() = %q, want %q", got, tt.want)
			}
		})
	}
}
