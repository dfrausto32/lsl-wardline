//go:build linux

package wg

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"os"
	"time"

	"github.com/vishvananda/netlink"
	"golang.zx2c4.com/wireguard/wgctrl"
	"golang.zx2c4.com/wireguard/wgctrl/wgtypes"
)

// Kernel drives the Linux kernel WireGuard module through netlink and wgctrl.
type Kernel struct {
	c *wgctrl.Client
}

func NewKernel() (*Kernel, error) {
	c, err := wgctrl.New()
	if err != nil {
		return nil, err
	}
	return &Kernel{c: c}, nil
}

func (k *Kernel) Close() error { return k.c.Close() }

func parseKey(s string) (wgtypes.Key, error) {
	raw, err := decodeKey(s)
	if err != nil {
		return wgtypes.Key{}, err
	}
	return wgtypes.NewKey(raw)
}

func (k *Kernel) Configure(_ context.Context, name string, cfg InterfaceConfig) error {
	priv, err := parseKey(cfg.PrivateKey)
	if err != nil {
		return err
	}
	if _, err := netlink.LinkByName(name); err != nil {
		var nf netlink.LinkNotFoundError
		if !errors.As(err, &nf) {
			return err
		}
		if err := netlink.LinkAdd(&netlink.Wireguard{LinkAttrs: netlink.LinkAttrs{Name: name}}); err != nil {
			return fmt.Errorf("create kernel WireGuard interface: %w", err)
		}
	}
	wc := wgtypes.Config{PrivateKey: &priv}
	if cfg.ListenPort != 0 {
		wc.ListenPort = &cfg.ListenPort
	}
	if err := k.c.ConfigureDevice(name, wc); err != nil {
		return err
	}
	if cfg.Address.IsValid() {
		return setAddress(name, cfg.Address)
	}
	return nil
}

func (k *Kernel) Peers(_ context.Context, name string) ([]PeerState, error) {
	d, err := k.c.Device(name)
	if err != nil {
		return nil, err
	}
	out := make([]PeerState, 0, len(d.Peers))
	for _, p := range d.Peers {
		s := PeerState{LastHandshake: p.LastHandshakeTime, RxBytes: p.ReceiveBytes, TxBytes: p.TransmitBytes}
		s.PublicKey = p.PublicKey.String()
		if p.PresharedKey != (wgtypes.Key{}) {
			s.PresharedKey = p.PresharedKey.String()
		}
		s.Keepalive = int(p.PersistentKeepaliveInterval / time.Second)
		if p.Endpoint != nil {
			s.Endpoint, s.LastEndpoint = p.Endpoint.String(), p.Endpoint.String()
		}
		for _, n := range p.AllowedIPs {
			pfx, ok := netip.AddrFromSlice(n.IP)
			if !ok {
				continue
			}
			ones, _ := n.Mask.Size()
			s.AllowedIPs = append(s.AllowedIPs, netip.PrefixFrom(pfx.Unmap(), ones))
		}
		s.AllowedIPs = sortedIPs(s.AllowedIPs)
		out = append(out, s)
	}
	sortPeers(out)
	return out, nil
}

func (k *Kernel) peerConfig(p PeerConfig) (wgtypes.PeerConfig, error) {
	pub, err := parseKey(p.PublicKey)
	if err != nil {
		return wgtypes.PeerConfig{}, err
	}
	var psk wgtypes.Key // zero value clears the preshared key
	if p.PresharedKey != "" {
		if psk, err = parseKey(p.PresharedKey); err != nil {
			return wgtypes.PeerConfig{}, err
		}
	}
	ka := time.Duration(p.Keepalive) * time.Second
	pc := wgtypes.PeerConfig{PublicKey: pub, PresharedKey: &psk, PersistentKeepaliveInterval: &ka, ReplaceAllowedIPs: true}
	for _, ip := range sortedIPs(p.AllowedIPs) {
		pc.AllowedIPs = append(pc.AllowedIPs, net.IPNet{IP: net.IP(ip.Addr().AsSlice()), Mask: net.CIDRMask(ip.Bits(), ip.Addr().BitLen())})
	}
	if p.Endpoint != "" {
		ep, err := net.ResolveUDPAddr("udp", p.Endpoint)
		if err != nil {
			return wgtypes.PeerConfig{}, err
		}
		pc.Endpoint = ep
	}
	return pc, nil
}

func (k *Kernel) ApplyDiff(_ context.Context, name string, d Diff) error {
	var peers []wgtypes.PeerConfig
	for _, p := range append(append([]PeerConfig{}, d.Add...), d.Update...) {
		pc, err := k.peerConfig(p)
		if err != nil {
			return err
		}
		peers = append(peers, pc)
	}
	for _, key := range d.Remove {
		pub, err := parseKey(key)
		if err != nil {
			return err
		}
		peers = append(peers, wgtypes.PeerConfig{PublicKey: pub, Remove: true})
	}
	if len(peers) == 0 {
		return nil
	}
	return k.c.ConfigureDevice(name, wgtypes.Config{Peers: peers})
}

func (k *Kernel) Remove(_ context.Context, name string) error {
	link, err := netlink.LinkByName(name)
	if err != nil {
		var nf netlink.LinkNotFoundError
		if errors.As(err, &nf) || errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}
	return netlink.LinkDel(link)
}

// KernelAvailable reports whether this machine can create kernel WireGuard
// interfaces, by creating and deleting a throwaway one.
func KernelAvailable() bool {
	const probe = "wgprobe0"
	netlink.LinkDel(&netlink.Wireguard{LinkAttrs: netlink.LinkAttrs{Name: probe}})
	if err := netlink.LinkAdd(&netlink.Wireguard{LinkAttrs: netlink.LinkAttrs{Name: probe}}); err != nil {
		return false
	}
	netlink.LinkDel(&netlink.Wireguard{LinkAttrs: netlink.LinkAttrs{Name: probe}})
	return true
}
