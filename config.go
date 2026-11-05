// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"time"
)

// maxIfaceName is the longest interface name the kernel accepts (IFNAMSIZ - 1).
const maxIfaceName = 15

type config struct {
	vip       *net.IPNet
	iface     string
	url       string
	interval  time.Duration
	timeout   time.Duration
	threshold int
	insecure  bool
}

func parseFlags(name string, args []string) (config, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)

	vip := fs.String("vip", "", "VIP to manage, in CIDR notation (e.g. 10.1.1.100/32); required")
	iface := fs.String("iface", "vip-api", "dummy interface to bind the VIP to; created if missing")
	url := fs.String("url", "https://127.0.0.1:6443/readyz", "health check URL; healthy means HTTP 200")
	interval := fs.Duration("interval", 2*time.Second, "health check interval")
	timeout := fs.Duration("timeout", 2*time.Second, "health check timeout")
	threshold := fs.Int("threshold", 2, "consecutive results needed to bind or withdraw the VIP")
	insecure := fs.Bool("insecure", true, "skip TLS verification of the health check URL")

	if err := fs.Parse(args); err != nil {
		return config{}, err
	}

	cfg := config{
		iface:     *iface,
		url:       *url,
		interval:  *interval,
		timeout:   *timeout,
		threshold: *threshold,
		insecure:  *insecure,
	}

	if *vip == "" {
		return config{}, errors.New("-vip is required")
	}

	ip, network, err := net.ParseCIDR(*vip)
	if err != nil {
		return config{}, fmt.Errorf("invalid -vip %q: %w", *vip, err)
	}

	// Keep the address itself, not the network address ParseCIDR returns alongside it.
	cfg.vip = &net.IPNet{IP: ip, Mask: network.Mask}

	switch {
	case cfg.iface == "" || len(cfg.iface) > maxIfaceName:
		return config{}, fmt.Errorf("invalid -iface %q: must be 1 to %d characters", cfg.iface, maxIfaceName)
	case cfg.url == "":
		return config{}, errors.New("-url must not be empty")
	case cfg.interval <= 0:
		return config{}, errors.New("-interval must be positive")
	case cfg.timeout <= 0:
		return config{}, errors.New("-timeout must be positive")
	case cfg.threshold < 1:
		return config{}, errors.New("-threshold must be at least 1")
	}

	return cfg, nil
}
