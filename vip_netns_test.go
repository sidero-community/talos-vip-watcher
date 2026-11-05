// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"sync/atomic"
	"testing"
	"time"

	"github.com/vishvananda/netlink"
)

// These tests create and delete interfaces, so they only run in a throwaway network namespace:
//
//	make test-netns
//
// which runs them under "unshare -rn" with TALOS_VIP_WATCHER_NETNS=1.
func requireNetns(t *testing.T) {
	t.Helper()

	if os.Getenv("TALOS_VIP_WATCHER_NETNS") != "1" {
		t.Skip("set TALOS_VIP_WATCHER_NETNS=1 in a throwaway network namespace to run (make test-netns)")
	}
}

func mustCIDR(t *testing.T, s string) *net.IPNet {
	t.Helper()

	ip, network, err := net.ParseCIDR(s)
	if err != nil {
		t.Fatal(err)
	}

	return &net.IPNet{IP: ip, Mask: network.Mask}
}

// hasAddr reports whether the interface carries the address, and whether it is up.
func hasAddr(t *testing.T, iface string, vip *net.IPNet) (present, up bool) {
	t.Helper()

	link, err := netlink.LinkByName(iface)
	if err != nil {
		return false, false
	}

	addrs, err := netlink.AddrList(link, netlink.FAMILY_ALL)
	if err != nil {
		t.Fatal(err)
	}

	for _, a := range addrs {
		if a.IP.Equal(vip.IP) {
			present = true
		}
	}

	return present, link.Attrs().Flags&net.FlagUp != 0
}

func TestNetnsVIP(t *testing.T) {
	requireNetns(t)

	for _, cidr := range []string{"10.1.1.100/32", "fd00::100/128"} {
		t.Run(cidr, func(t *testing.T) {
			vip := mustCIDR(t, cidr)

			iface := "vip-test4"
			if vip.IP.To4() == nil {
				iface = "vip-test6"
			}

			v := newNetlinkVIP(iface, vip)

			// Withdrawing what was never created is not an error.
			if err := v.Withdraw(); err != nil {
				t.Fatalf("withdraw before create: %v", err)
			}

			for range 2 { // idempotent
				if err := v.Ensure(); err != nil {
					t.Fatalf("ensure: %v", err)
				}
			}

			link, err := netlink.LinkByName(iface)
			if err != nil {
				t.Fatal(err)
			}

			if link.Type() != "dummy" {
				t.Fatalf("created a %s interface, want dummy", link.Type())
			}

			if present, up := hasAddr(t, iface, vip); !present || !up {
				t.Fatalf("after ensure: present=%v up=%v", present, up)
			}

			// An address removed behind the watcher's back is restored by the next ensure.
			if err := netlink.AddrDel(link, &netlink.Addr{IPNet: vip}); err != nil {
				t.Fatal(err)
			}

			if err := v.Ensure(); err != nil {
				t.Fatal(err)
			}

			if present, _ := hasAddr(t, iface, vip); !present {
				t.Fatal("ensure did not restore a removed address")
			}

			for range 2 { // idempotent
				if err := v.Withdraw(); err != nil {
					t.Fatalf("withdraw: %v", err)
				}
			}

			if present, up := hasAddr(t, iface, vip); present || up {
				t.Fatalf("after withdraw: present=%v up=%v", present, up)
			}
		})
	}
}

// A real interface named by mistake is never touched: withdrawing takes the interface down.
func TestNetnsVIPRefusesANonDummyInterface(t *testing.T) {
	requireNetns(t)

	v := newNetlinkVIP("lo", mustCIDR(t, "10.1.1.100/32"))

	if err := v.Ensure(); err == nil {
		t.Fatal("ensure accepted the loopback interface")
	}

	if err := v.Withdraw(); err == nil {
		t.Fatal("withdraw accepted the loopback interface")
	}
}

// End to end: the VIP follows the health endpoint, and is withdrawn on shutdown.
func TestNetnsRun(t *testing.T) {
	requireNetns(t)

	lo, err := netlink.LinkByName("lo")
	if err != nil {
		t.Fatal(err)
	}

	if err := netlink.LinkSetUp(lo); err != nil {
		t.Fatal(err)
	}

	var ready atomic.Bool

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		if !ready.Load() {
			w.WriteHeader(http.StatusServiceUnavailable)
		}
	}))
	t.Cleanup(srv.Close)

	cfg := config{
		vip:       mustCIDR(t, "10.1.1.100/32"),
		iface:     "vip-run",
		url:       srv.URL + "/readyz",
		interval:  10 * time.Millisecond,
		timeout:   time.Second,
		threshold: 2,
		insecure:  true,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)

	go func() {
		done <- run(ctx, cfg, newHTTPChecker(cfg.url, cfg.timeout, cfg.insecure), newNetlinkVIP(cfg.iface, cfg.vip))
	}()

	waitFor := func(want bool) {
		t.Helper()

		deadline := time.Now().Add(5 * time.Second)

		for {
			present, _ := hasAddr(t, cfg.iface, cfg.vip)
			if present == want {
				return
			}

			if time.Now().After(deadline) {
				t.Fatalf("VIP present=%v, want %v", present, want)
			}

			time.Sleep(5 * time.Millisecond)
		}
	}

	time.Sleep(100 * time.Millisecond)
	waitFor(false)

	ready.Store(true)
	waitFor(true)

	ready.Store(false)
	waitFor(false)

	ready.Store(true)
	waitFor(true)

	cancel()

	if err := <-done; err != nil {
		t.Fatal(err)
	}

	if present, up := hasAddr(t, cfg.iface, cfg.vip); present || up {
		t.Fatalf("after shutdown: present=%v up=%v", present, up)
	}
}
