// Copyright 2025 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"strings"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// These are pure-unit tests (no API server, no build tag): they run in the fast
// `make test-unit` layer and cover the safety-critical branches that were
// previously reachable only through the slow envtest suite (#68).

func TestAllocateMonID(t *testing.T) {
	tests := []struct {
		name    string
		prefix  string
		taken   []string
		want    string
		wantErr bool
	}{
		{name: "first free when none taken", prefix: "ext-", taken: nil, want: "ext-a"},
		{name: "skips a single collision", prefix: "ext-", taken: []string{"ext-a"}, want: "ext-b"},
		{name: "skips a run of collisions", prefix: "ext-", taken: []string{"ext-a", "ext-b", "ext-c"}, want: "ext-d"},
		{name: "ignores other prefixes", prefix: "ext-", taken: []string{"other-a", "x-b"}, want: "ext-a"},
		{name: "last suffix available", prefix: "ext-", taken: allSuffixesExcept("ext-", 'z'), want: "ext-z"},
		{name: "exhaustion errors", prefix: "ext-", taken: allSuffixes("ext-"), wantErr: true},
		{name: "empty prefix works", prefix: "", taken: []string{"a"}, want: "b"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := allocateMonID(tt.prefix, tt.taken)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got id %q", got)
				}
				// exhaustion error must name the prefix so operators can diagnose it
				if !strings.Contains(err.Error(), tt.prefix) {
					t.Errorf("error %q should mention prefix %q", err, tt.prefix)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func allSuffixes(prefix string) []string {
	var out []string
	for c := byte('a'); c <= 'z'; c++ {
		out = append(out, prefix+string(c))
	}
	return out
}

func allSuffixesExcept(prefix string, keep byte) []string {
	var out []string
	for c := byte('a'); c <= 'z'; c++ {
		if c != keep {
			out = append(out, prefix+string(c))
		}
	}
	return out
}

func TestDeterminePublicAddressFor(t *testing.T) {
	lbWithIPv4 := &corev1.Service{
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: "10.0.0.5"}},
		}},
	}
	lbIPv6Only := &corev1.Service{
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: "2001:db8::1"}},
		}},
	}
	lbSkipsBadThenIPv4 := &corev1.Service{
		Spec: corev1.ServiceSpec{Type: corev1.ServiceTypeLoadBalancer},
		Status: corev1.ServiceStatus{LoadBalancer: corev1.LoadBalancerStatus{
			Ingress: []corev1.LoadBalancerIngress{{IP: "not-an-ip"}, {IP: "2001:db8::1"}, {IP: "10.0.0.9"}},
		}},
	}

	tests := []struct {
		name    string
		service *corev1.Service
		nodeIP  string
		want    string
		wantErr bool
	}{
		{name: "nil service uses pod ip substitution", service: nil, want: "$(ROOK_POD_IP)"},
		{name: "clusterip allocated", service: svc(corev1.ServiceTypeClusterIP, "172.16.0.3"), want: "172.16.0.3"},
		{name: "clusterip not yet allocated errors", service: svc(corev1.ServiceTypeClusterIP, ""), wantErr: true},
		{name: "nodeport returns configured node ip", service: svc(corev1.ServiceTypeNodePort, ""), nodeIP: "192.168.1.10", want: "192.168.1.10"},
		{name: "loadbalancer ipv4", service: lbWithIPv4, want: "10.0.0.5"},
		{name: "loadbalancer skips bad and ipv6 then takes ipv4", service: lbSkipsBadThenIPv4, want: "10.0.0.9"},
		{name: "loadbalancer no ingress errors", service: svc(corev1.ServiceTypeLoadBalancer, ""), wantErr: true},
		{name: "loadbalancer ipv6 only errors", service: lbIPv6Only, wantErr: true},
		{name: "unknown type errors", service: svc("Weird", ""), wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := determinePublicAddressFor(tt.service, tt.nodeIP)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error, got address %q", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if got != tt.want {
				t.Errorf("got %q, want %q", got, tt.want)
			}
		})
	}
}

func svc(t corev1.ServiceType, clusterIP string) *corev1.Service {
	return &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{Name: "arbiter"},
		Spec:       corev1.ServiceSpec{Type: t, ClusterIP: clusterIP},
	}
}
