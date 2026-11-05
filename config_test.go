// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"strings"
	"testing"
	"time"
)

func TestParseFlags(t *testing.T) {
	t.Parallel()

	cfg, err := parseFlags("test", []string{"--vip=10.1.1.100/32", "--iface=vip-api", "--interval=5s"})
	if err != nil {
		t.Fatal(err)
	}

	if got := cfg.vip.String(); got != "10.1.1.100/32" {
		t.Errorf("vip: got %s", got)
	}

	if cfg.interval != 5*time.Second || cfg.timeout != 2*time.Second || cfg.threshold != 2 || !cfg.insecure {
		t.Errorf("unexpected defaults: %+v", cfg)
	}

	// The address is kept, not reduced to its network.
	cfg, err = parseFlags("test", []string{"-vip=10.1.1.100/24"})
	if err != nil {
		t.Fatal(err)
	}

	if got := cfg.vip.String(); got != "10.1.1.100/24" {
		t.Errorf("vip: got %s, want the address kept", got)
	}
}

func TestParseFlagsRejects(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		args []string
		want string
	}{
		"missing vip":    {args: nil, want: "-vip is required"},
		"bad vip":        {args: []string{"-vip=10.1.1.100"}, want: "invalid -vip"},
		"long iface":     {args: []string{"-vip=10.1.1.100/32", "-iface=a-very-long-interface"}, want: "invalid -iface"},
		"zero interval":  {args: []string{"-vip=10.1.1.100/32", "-interval=0s"}, want: "-interval must be positive"},
		"zero threshold": {args: []string{"-vip=10.1.1.100/32", "-threshold=0"}, want: "-threshold must be at least 1"},
		"empty url":      {args: []string{"-vip=10.1.1.100/32", "-url="}, want: "-url must not be empty"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := parseFlags("test", tc.args)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}
