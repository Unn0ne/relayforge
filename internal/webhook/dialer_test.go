package webhook

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"testing"
)

func TestSafeDialerRejectsPrivateAddress(t *testing.T) {
	dialer := &dialerStub{}
	safe := NewSafeDialer(resolverStub{addresses: []netip.Addr{netip.MustParseAddr("127.0.0.1")}}, dialer, false)

	_, err := safe.DialContext(context.Background(), "tcp", "webhook.example:443")
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("error = %v", err)
	}
	if dialer.called {
		t.Fatal("blocked address was dialed")
	}
}

func TestSafeDialerRejectsMixedResolution(t *testing.T) {
	dialer := &dialerStub{}
	safe := NewSafeDialer(resolverStub{addresses: []netip.Addr{
		netip.MustParseAddr("93.184.216.34"),
		netip.MustParseAddr("10.0.0.1"),
	}}, dialer, false)

	_, err := safe.DialContext(context.Background(), "tcp", "webhook.example:443")
	if !errors.Is(err, ErrBlockedAddress) {
		t.Fatalf("error = %v", err)
	}
	if dialer.called {
		t.Fatal("mixed resolution was dialed")
	}
}

func TestAddressPolicy(t *testing.T) {
	tests := []struct {
		address      string
		allowPrivate bool
		want         bool
	}{
		{address: "93.184.216.34", want: true},
		{address: "10.0.0.1", want: false},
		{address: "100.64.0.1", want: false},
		{address: "169.254.169.254", want: false},
		{address: "127.0.0.1", want: false},
		{address: "::1", want: false},
		{address: "fc00::1", want: false},
		{address: "127.0.0.1", allowPrivate: true, want: true},
		{address: "0.0.0.0", allowPrivate: true, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.address, func(t *testing.T) {
			got := addressAllowed(netip.MustParseAddr(tt.address), tt.allowPrivate)
			if got != tt.want {
				t.Fatalf("addressAllowed() = %v, want %v", got, tt.want)
			}
		})
	}
}

type resolverStub struct {
	addresses []netip.Addr
	err       error
}

func (r resolverStub) LookupNetIP(context.Context, string, string) ([]netip.Addr, error) {
	return r.addresses, r.err
}

type dialerStub struct {
	called bool
}

func (d *dialerStub) DialContext(context.Context, string, string) (net.Conn, error) {
	d.called = true
	return nil, errors.New("dialed")
}
