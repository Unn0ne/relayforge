package webhook

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
)

var ErrBlockedAddress = errors.New("target address is blocked")

var blockedPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::ffff:0:0/96"),
}

type Resolver interface {
	LookupNetIP(context.Context, string, string) ([]netip.Addr, error)
}

type ContextDialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type SafeDialer struct {
	resolver     Resolver
	dialer       ContextDialer
	allowPrivate bool
}

func NewSafeDialer(resolver Resolver, dialer ContextDialer, allowPrivate bool) *SafeDialer {
	return &SafeDialer{resolver: resolver, dialer: dialer, allowPrivate: allowPrivate}
}

func (d *SafeDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("split target address: %w", err)
	}

	addresses, err := d.resolve(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, current := range addresses {
		if !addressAllowed(current, d.allowPrivate) {
			return nil, fmt.Errorf("%w: %s", ErrBlockedAddress, current)
		}
	}

	var dialErrors []error
	for _, current := range addresses {
		current = current.Unmap()
		if (network == "tcp4" && !current.Is4()) || (network == "tcp6" && !current.Is6()) {
			continue
		}
		connection, dialErr := d.dialer.DialContext(ctx, network, net.JoinHostPort(current.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		dialErrors = append(dialErrors, dialErr)
	}
	if len(dialErrors) == 0 {
		return nil, errors.New("target has no compatible addresses")
	}
	return nil, fmt.Errorf("dial target: %w", errors.Join(dialErrors...))
}

func (d *SafeDialer) resolve(ctx context.Context, host string) ([]netip.Addr, error) {
	if parsed, err := netip.ParseAddr(strings.Trim(host, "[]")); err == nil {
		return []netip.Addr{parsed}, nil
	}
	addresses, err := d.resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve target: %w", err)
	}
	if len(addresses) == 0 {
		return nil, errors.New("target resolved to no addresses")
	}
	return addresses, nil
}

func addressAllowed(address netip.Addr, allowPrivate bool) bool {
	if !address.IsValid() {
		return false
	}
	address = address.Unmap()
	if address.IsUnspecified() || address.IsMulticast() {
		return false
	}
	if allowPrivate {
		return true
	}
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() || address.IsLinkLocalUnicast() {
		return false
	}
	for _, prefix := range blockedPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}
