// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"errors"
	"fmt"
	"net"

	"github.com/vishvananda/netlink"
	"golang.org/x/sys/unix"
)

// netlinkVIP binds a VIP to a dummy interface and removes it again. Both operations are
// idempotent, so they can be applied on every check.
type netlinkVIP struct {
	iface string
	addr  *netlink.Addr
}

func newNetlinkVIP(iface string, vip *net.IPNet) *netlinkVIP {
	addr := &netlink.Addr{IPNet: vip}

	if vip.IP.To4() == nil {
		// An IPv6 address would otherwise stay tentative during duplicate address detection.
		addr.Flags = unix.IFA_F_NODAD
	}

	return &netlinkVIP{iface: iface, addr: addr}
}

// Ensure creates the dummy interface if needed, brings it up and binds the VIP to it.
func (v *netlinkVIP) Ensure() error {
	link, err := v.link()
	if err != nil {
		return err
	}

	if link == nil {
		attrs := netlink.NewLinkAttrs()
		attrs.Name = v.iface

		if err := netlink.LinkAdd(&netlink.Dummy{LinkAttrs: attrs}); err != nil && !errors.Is(err, unix.EEXIST) {
			return fmt.Errorf("creating dummy interface %s: %w", v.iface, err)
		}

		if link, err = v.link(); err != nil {
			return err
		}

		if link == nil {
			return fmt.Errorf("dummy interface %s vanished after creation", v.iface)
		}
	}

	if err := netlink.LinkSetUp(link); err != nil {
		return fmt.Errorf("bringing up %s: %w", v.iface, err)
	}

	if err := netlink.AddrReplace(link, v.addr); err != nil {
		return fmt.Errorf("adding %s to %s: %w", v.addr.IPNet, v.iface, err)
	}

	return nil
}

// Withdraw removes the VIP from the dummy interface and takes the interface down. A missing
// interface or address is not an error.
func (v *netlinkVIP) Withdraw() error {
	link, err := v.link()
	if err != nil || link == nil {
		return err
	}

	if err := netlink.AddrDel(link, v.addr); err != nil && !errors.Is(err, unix.EADDRNOTAVAIL) && !errors.Is(err, unix.ENOENT) {
		return fmt.Errorf("removing %s from %s: %w", v.addr.IPNet, v.iface, err)
	}

	if err := netlink.LinkSetDown(link); err != nil {
		return fmt.Errorf("taking down %s: %w", v.iface, err)
	}

	return nil
}

// link returns the managed interface, or nil if it does not exist. It refuses to manage an
// interface that is not a dummy: withdrawing takes the interface down, which must never
// happen to a real uplink named by mistake.
func (v *netlinkVIP) link() (netlink.Link, error) {
	link, err := netlink.LinkByName(v.iface)
	if err != nil {
		var notFound netlink.LinkNotFoundError
		if errors.As(err, &notFound) {
			return nil, nil
		}

		return nil, fmt.Errorf("looking up %s: %w", v.iface, err)
	}

	if link.Type() != "dummy" {
		return nil, fmt.Errorf("interface %s is a %s interface, refusing to manage anything but a dummy interface", v.iface, link.Type())
	}

	return link, nil
}
