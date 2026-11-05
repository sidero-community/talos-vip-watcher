// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Command talos-vip-watcher binds a virtual IP to a dummy interface while the local Kubernetes
// API server is ready, and withdraws it while it is not. A BGP speaker that exports the
// interface's addresses then advertises the VIP from healthy control plane nodes only.
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"syscall"
)

// version is set at build time.
var version = "dev"

func main() {
	log.SetFlags(log.LstdFlags | log.LUTC)

	cfg, err := parseFlags(os.Args[0], os.Args[1:])
	if err != nil {
		log.Fatal(err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	log.Printf("talos-vip-watcher %s: managing VIP %s on interface %s, checking %s every %s",
		version, cfg.vip, cfg.iface, cfg.url, cfg.interval)

	vip := newNetlinkVIP(cfg.iface, cfg.vip)
	checker := newHTTPChecker(cfg.url, cfg.timeout, cfg.insecure)

	if err := run(ctx, cfg, checker, vip); err != nil {
		log.Fatal(err)
	}
}
