// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
	"time"
)

func TestDebouncer(t *testing.T) {
	t.Parallel()

	type step struct {
		healthy, active, changed bool
	}

	for name, tc := range map[string]struct {
		threshold int
		steps     []step
	}{
		"binds after two passes, withdraws after two failures": {
			threshold: 2,
			steps: []step{
				{healthy: true, active: false},
				{healthy: true, active: true, changed: true},
				{healthy: true, active: true},
				{healthy: false, active: true},
				{healthy: false, active: false, changed: true},
				{healthy: false, active: false},
			},
		},
		"an interrupted streak starts over": {
			threshold: 2,
			steps: []step{
				{healthy: true, active: false},
				{healthy: false, active: false},
				{healthy: true, active: false},
				{healthy: true, active: true, changed: true},
				{healthy: false, active: true},
				{healthy: true, active: true},
				{healthy: false, active: true},
				{healthy: false, active: false, changed: true},
			},
		},
		"threshold one follows every result": {
			threshold: 1,
			steps: []step{
				{healthy: true, active: true, changed: true},
				{healthy: false, active: false, changed: true},
				{healthy: false, active: false},
			},
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			d := debouncer{threshold: tc.threshold}

			for i, s := range tc.steps {
				active, changed := d.observe(s.healthy)
				if active != s.active || changed != s.changed {
					t.Fatalf("step %d (healthy=%v): got active=%v changed=%v, want active=%v changed=%v",
						i, s.healthy, active, changed, s.active, s.changed)
				}
			}
		})
	}
}

// scriptedChecker answers health checks from a script, then repeats its last answer.
type scriptedChecker struct {
	mu      sync.Mutex
	results []error
	calls   int
}

func (c *scriptedChecker) Check(context.Context) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	i := min(c.calls, len(c.results)-1)
	c.calls++

	return c.results[i]
}

func (c *scriptedChecker) callCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.calls
}

// recordingVIP records what run asked of the VIP.
type recordingVIP struct {
	mu  sync.Mutex
	ops []string
}

func (v *recordingVIP) Ensure() error   { return v.record("ensure") }
func (v *recordingVIP) Withdraw() error { return v.record("withdraw") }

func (v *recordingVIP) record(op string) error {
	v.mu.Lock()
	defer v.mu.Unlock()

	v.ops = append(v.ops, op)

	return nil
}

func (v *recordingVIP) recorded() []string {
	v.mu.Lock()
	defer v.mu.Unlock()

	return append([]string(nil), v.ops...)
}

func testConfig() config {
	return config{
		vip:       &net.IPNet{IP: net.ParseIP("10.1.1.100"), Mask: net.CIDRMask(32, 32)},
		iface:     "vip-test",
		url:       "https://127.0.0.1:6443/readyz",
		interval:  time.Millisecond,
		timeout:   time.Second,
		threshold: 2,
	}
}

// run starts withdrawn, applies the debounced state on every check and withdraws on shutdown.
func TestRun(t *testing.T) {
	t.Parallel()

	down := errors.New("connection refused")
	health := &scriptedChecker{results: []error{nil, nil, nil, down, down, down}}
	vip := &recordingVIP{}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() { done <- run(ctx, testConfig(), health, vip) }()

	deadline := time.Now().Add(5 * time.Second)
	for health.callCount() < 6 {
		if time.Now().After(deadline) {
			t.Fatal("run did not check health six times")
		}

		time.Sleep(time.Millisecond)
	}

	cancel()

	if err := <-done; err != nil {
		t.Fatalf("run returned %v", err)
	}

	ops := vip.recorded()

	// Startup, then one operation per check, then shutdown.
	want := []string{
		"withdraw",                     // a VIP left by a previous instance is removed first
		"withdraw", "ensure", "ensure", // pass (not yet), pass (bind), pass
		"ensure", "withdraw", "withdraw", // fail (still bound), fail (withdraw), fail
	}

	if len(ops) < len(want)+1 {
		t.Fatalf("got operations %v, want at least %v followed by a shutdown withdraw", ops, want)
	}

	for i, op := range want {
		if ops[i] != op {
			t.Fatalf("operation %d: got %v, want %v as a prefix", i, ops, want)
		}
	}

	if ops[len(ops)-1] != "withdraw" {
		t.Fatalf("the last operation is %q, want the shutdown withdraw: %v", ops[len(ops)-1], ops)
	}
}

// failingVIP fails every operation.
type failingVIP struct{ err error }

func (v failingVIP) Ensure() error   { return v.err }
func (v failingVIP) Withdraw() error { return v.err }

// A VIP that cannot be withdrawn at startup stops the watcher: running on would leave the state
// unknown, possibly advertising from an unhealthy node.
func TestRunFailsWhenTheLeftoverVIPCannotBeWithdrawn(t *testing.T) {
	t.Parallel()

	err := run(context.Background(), testConfig(), &scriptedChecker{results: []error{nil}},
		failingVIP{err: errors.New("operation not permitted")})
	if err == nil {
		t.Fatal("run succeeded, want an error")
	}
}
