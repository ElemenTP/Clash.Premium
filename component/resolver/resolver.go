package resolver

import (
	"context"
	"errors"
	"math/rand"
	"net"
	"net/netip"
	"time"

	"github.com/Dreamacro/clash/common/nnip"
	"github.com/Dreamacro/clash/component/trie"
)

var (
	// DefaultResolver aim to resolve ip
	DefaultResolver Resolver

	// ProxyServerHostResolver resolve ip to proxies server host
	ProxyServerHostResolver Resolver

	// DisableIPv6 means don't resolve ipv6 host
	// default value is true
	DisableIPv6 = true

	// DefaultHosts aim to resolve hosts
	DefaultHosts = trie.New[netip.Addr]()

	// DefaultDNSTimeout defined the default dns request timeout
	DefaultDNSTimeout = time.Second * 5
)

var (
	ErrIPNotFound   = errors.New("couldn't find ip")
	ErrIPVersion    = errors.New("ip version error")
	ErrIPv6Disabled = errors.New("ipv6 disabled")
)

const firstIPKey = ipContextKey("key-lookup-first-ip")

type ipContextKey string

type Resolver interface {
	ResolveIP(ctx context.Context, host string) (ip netip.Addr, err error)
	ResolveIPv4(ctx context.Context, host string) (ip netip.Addr, err error)
	ResolveIPv6(ctx context.Context, host string) (ip netip.Addr, err error)
}

// ResolveIPv4 with a host, return ipv4
func ResolveIPv4(host string) (netip.Addr, error) {
	return resolveIPv4(context.Background(), host)
}

func ResolveIPv4WithResolver(ctx context.Context, host string, r Resolver) (netip.Addr, error) {
	if node := DefaultHosts.Search(host); node != nil {
		if ip := node.Data; ip.Is4() {
			return ip, nil
		}
	}

	ip, err := netip.ParseAddr(host)
	if err == nil {
		ip = ip.Unmap()
		if ip.Is4() {
			return ip, nil
		}
		return netip.Addr{}, ErrIPVersion
	}

	if r != nil {
		return r.ResolveIPv4(ctx, host)
	}

	if DefaultResolver == nil {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, DefaultDNSTimeout)
			defer cancel()
		}

		ipAddrs, err := net.DefaultResolver.LookupIP(ctx, "ip4", host)
		length := len(ipAddrs)
		if err != nil {
			return netip.Addr{}, err
		} else if length == 0 {
			return netip.Addr{}, ErrIPNotFound
		}

		index := 0
		if length > 1 && ShouldRandomIP(ctx) {
			index = rand.Intn(length)
		}

		ip := ipAddrs[index].To4()
		if ip == nil {
			return netip.Addr{}, ErrIPVersion
		}

		return netip.AddrFrom4(*(*[4]byte)(ip)), nil
	}

	return netip.Addr{}, ErrIPNotFound
}

// ResolveIPv6 with a host, return ipv6
func ResolveIPv6(host string) (netip.Addr, error) {
	return ResolveIPv6WithResolver(context.Background(), host, DefaultResolver)
}

func ResolveIPv6WithResolver(ctx context.Context, host string, r Resolver) (netip.Addr, error) {
	if DisableIPv6 {
		return netip.Addr{}, ErrIPv6Disabled
	}

	if node := DefaultHosts.Search(host); node != nil {
		if ip := node.Data; ip.Is6() {
			return ip, nil
		}
	}

	ip, err := netip.ParseAddr(host)
	if err == nil {
		if ip.Is6() {
			return ip, nil
		}
		return netip.Addr{}, ErrIPVersion
	}

	if r != nil {
		return r.ResolveIPv6(ctx, host)
	}

	if DefaultResolver == nil {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, DefaultDNSTimeout)
			defer cancel()
		}

		ipAddrs, err := net.DefaultResolver.LookupIP(ctx, "ip6", host)
		length := len(ipAddrs)
		if err != nil {
			return netip.Addr{}, err
		} else if length == 0 {
			return netip.Addr{}, ErrIPNotFound
		}

		index := 0
		if length > 1 && ShouldRandomIP(ctx) {
			index = rand.Intn(length)
		}

		ip = nnip.IpToAddr(ipAddrs[index])
		if !ip.IsValid() {
			return netip.Addr{}, ErrIPNotFound
		}

		return ip, nil
	}

	return netip.Addr{}, ErrIPNotFound
}

// ResolveIPWithResolver same as ResolveIP, but with a resolver
func ResolveIPWithResolver(ctx context.Context, host string, r Resolver) (netip.Addr, error) {
	if node := DefaultHosts.Search(host); node != nil {
		return node.Data, nil
	}

	if r != nil {
		if DisableIPv6 {
			return r.ResolveIPv4(ctx, host)
		}
		return r.ResolveIP(ctx, host)
	} else if DisableIPv6 {
		return resolveIPv4(ctx, host)
	}

	ip, err := netip.ParseAddr(host)
	if err == nil {
		return ip, nil
	}

	if DefaultResolver == nil {
		if _, ok := ctx.Deadline(); !ok {
			var cancel context.CancelFunc
			ctx, cancel = context.WithTimeout(ctx, DefaultDNSTimeout)
			defer cancel()
		}

		ipAddrs, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
		length := len(ipAddrs)
		if err != nil {
			return netip.Addr{}, err
		} else if length == 0 {
			return netip.Addr{}, ErrIPNotFound
		}

		index := 0
		if length > 1 && ShouldRandomIP(ctx) {
			index = rand.Intn(length)
		}

		ip = nnip.IpToAddr(ipAddrs[index])
		if !ip.IsValid() {
			return netip.Addr{}, ErrIPNotFound
		}

		return ip, nil
	}

	return netip.Addr{}, ErrIPNotFound
}

// ResolveIP with a host, return ip
func ResolveIP(host string) (netip.Addr, error) {
	return resolveIP(context.Background(), host)
}

// ResolveFirstIP with a host, return ip
func ResolveFirstIP(host string) (netip.Addr, error) {
	ctx := context.WithValue(context.Background(), firstIPKey, struct{}{})
	return resolveIP(ctx, host)
}

// ResolveIPv4ProxyServerHost proxies server host only
func ResolveIPv4ProxyServerHost(host string) (netip.Addr, error) {
	if ProxyServerHostResolver != nil {
		return ResolveIPv4WithResolver(context.Background(), host, ProxyServerHostResolver)
	}
	return ResolveIPv4(host)
}

// ResolveIPv6ProxyServerHost proxies server host only
func ResolveIPv6ProxyServerHost(host string) (netip.Addr, error) {
	if ProxyServerHostResolver != nil {
		return ResolveIPv6WithResolver(context.Background(), host, ProxyServerHostResolver)
	}
	return ResolveIPv6(host)
}

// ResolveProxyServerHost proxies server host only
func ResolveProxyServerHost(host string) (netip.Addr, error) {
	if ProxyServerHostResolver != nil {
		return ResolveIPWithResolver(context.Background(), host, ProxyServerHostResolver)
	}
	return ResolveIP(host)
}

func resolveIP(ctx context.Context, host string) (netip.Addr, error) {
	return ResolveIPWithResolver(ctx, host, DefaultResolver)
}

func resolveIPv4(ctx context.Context, host string) (netip.Addr, error) {
	return ResolveIPv4WithResolver(ctx, host, DefaultResolver)
}

func ShouldRandomIP(ctx context.Context) bool {
	return ctx.Value(firstIPKey) == nil
}
