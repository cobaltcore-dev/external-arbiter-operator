// Copyright 2026 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/util/intstr"
)

func TestArbiterServicePorts(t *testing.T) {
	tests := []struct {
		name        string
		serviceType corev1.ServiceType
	}{
		{name: "cluster ip services have no node ports", serviceType: corev1.ServiceTypeClusterIP},
		{name: "load balancer services have no node ports", serviceType: corev1.ServiceTypeLoadBalancer},
		{name: "node port services pin the node ports to the ceph messenger ports", serviceType: corev1.ServiceTypeNodePort},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := arbiterServicePorts(tt.serviceType)
			if len(got) != 2 {
				t.Fatalf("arbiterServicePorts() returned %d ports, want 2", len(got))
			}

			want := map[string]int32{"tcp-msgr1": 6789, "tcp-msgr2": 3300}
			portsByName := make(map[string]corev1.ServicePort, len(got))
			for _, port := range got {
				portsByName[port.Name] = port
			}

			for name, number := range want {
				port, ok := portsByName[name]
				if !ok {
					t.Errorf("arbiterServicePorts() is missing port %s, got %v", name, got)
					continue
				}
				if port.Port != number {
					t.Errorf("port %s: Port = %d, want %d", name, port.Port, number)
				}
				if port.Protocol != corev1.ProtocolTCP {
					t.Errorf("port %s: Protocol = %s, want TCP", name, port.Protocol)
				}
				if !reflect.DeepEqual(port.TargetPort, intstr.FromInt32(number)) {
					t.Errorf("port %s: TargetPort = %s, want %d", name, port.TargetPort.String(), number)
				}
				if tt.serviceType == corev1.ServiceTypeNodePort && port.NodePort != number {
					t.Errorf("port %s: NodePort = %d, want it pinned to %d", name, port.NodePort, number)
				}
				if tt.serviceType != corev1.ServiceTypeNodePort && port.NodePort != 0 {
					t.Errorf("port %s: NodePort = %d, want 0 for %s services", name, port.NodePort, tt.serviceType)
				}
			}
		})
	}
}
