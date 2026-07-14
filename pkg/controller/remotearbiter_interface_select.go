// Copyright 2026 SAP SE or an SAP affiliate company and cobaltcore-dev contributors
// SPDX-License-Identifier: Apache-2.0

package controller

import (
	"fmt"
	"net"
	"net/netip"
)

func PreferedSourceIP(destination string) (netip.Addr, error) {
	conn, err := net.Dial("udp", destination)
	if err != nil {
		return netip.Addr{}, fmt.Errorf("rout lookup for %s: %w", destination, err)
	}

	defer conn.Close()

	udpAddr, ok := conn.LocalAddr().(*net.UDPAddr)

	if !ok {
		return netip.Addr{}, fmt.Errorf("unexpected local address type %T", conn.LocalAddr())
	}

	ip, ok := netip.AddrFromSlice(udpAddr.IP)

	if !ok {
		return netip.Addr{}, fmt.Errorf("invalid local IP %q", udpAddr.IP)
	}

	return ip.Unmap(), nil
}
