// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"context"
	"fmt"
	"log"
	"time"
)

type checker interface {
	Check(ctx context.Context) error
}

type vipManager interface {
	Ensure() error
	Withdraw() error
}

// debouncer turns health results into the desired VIP state. The state only changes after
// threshold consecutive results that disagree with it, so a single failed or successful check
// does not make the VIP flap.
type debouncer struct {
	threshold int
	active    bool
	streak    int
}

// observe records a health result and returns the desired state, and whether it just changed.
func (d *debouncer) observe(healthy bool) (active, changed bool) {
	if healthy == d.active {
		d.streak = 0

		return d.active, false
	}

	d.streak++
	if d.streak < d.threshold {
		return d.active, false
	}

	d.active = healthy
	d.streak = 0

	return d.active, true
}

// run checks health every interval and applies the desired VIP state, until ctx is done.
//
// It starts withdrawn: a VIP left behind by a previous instance that crashed would otherwise stay
// advertised from a node that may no longer be healthy. The state is applied on every check, not
// only when it changes, so that an address removed by someone else is restored. On shutdown the
// VIP is withdrawn.
func run(ctx context.Context, cfg config, health checker, vip vipManager) error {
	if err := vip.Withdraw(); err != nil {
		return fmt.Errorf("withdrawing a VIP left behind by a previous instance: %w", err)
	}

	defer func() {
		if err := vip.Withdraw(); err != nil {
			log.Printf("failed to withdraw VIP %s on shutdown: %v", cfg.vip, err)

			return
		}

		log.Printf("VIP %s withdrawn from %s on shutdown", cfg.vip, cfg.iface)
	}()

	ticker := time.NewTicker(cfg.interval)
	defer ticker.Stop()

	d := debouncer{threshold: cfg.threshold}

	var (
		lastHealth = "unknown"
		lastApply  string
	)

	for {
		select {
		case <-ctx.Done():
			log.Printf("shutting down")

			return nil
		case <-ticker.C:
		}

		checkCtx, cancel := context.WithTimeout(ctx, cfg.timeout)
		err := health.Check(checkCtx)
		cancel()

		// Log health only when it changes, not on every check.
		status := "healthy"
		if err != nil {
			status = err.Error()
		}

		if status != lastHealth {
			if err != nil {
				log.Printf("health check failing: %v", err)
			} else {
				log.Printf("health check passing")
			}

			lastHealth = status
		}

		active, changed := d.observe(err == nil)

		var applyErr error
		if active {
			applyErr = vip.Ensure()
		} else {
			applyErr = vip.Withdraw()
		}

		switch {
		case applyErr != nil:
			if applyErr.Error() != lastApply {
				log.Printf("failed to apply VIP state: %v", applyErr)
			}

			lastApply = applyErr.Error()
		case changed && active:
			log.Printf("VIP %s bound to %s after %d passing checks", cfg.vip, cfg.iface, cfg.threshold)

			lastApply = ""
		case changed:
			log.Printf("VIP %s withdrawn from %s after %d failing checks", cfg.vip, cfg.iface, cfg.threshold)

			lastApply = ""
		default:
			lastApply = ""
		}
	}
}
