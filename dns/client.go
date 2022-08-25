package dns

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/netip"
	"strings"

	D "github.com/miekg/dns"

	"github.com/Dreamacro/clash/component/dialer"
	"github.com/Dreamacro/clash/component/resolver"
)

type client struct {
	*D.Client
	r            *Resolver
	port         string
	host         string
	iface        string
	proxyAdapter string
}

func (c *client) Exchange(m *D.Msg) (*D.Msg, error) {
	return c.ExchangeContext(context.Background(), m)
}

func (c *client) ExchangeContext(ctx context.Context, m *D.Msg) (*D.Msg, error) {
	var (
		ip  netip.Addr
		err error
	)
	if ip, err = netip.ParseAddr(c.host); err != nil {
		if c.r == nil {
			return nil, fmt.Errorf("dns %s not a valid ip", c.host)
		} else {
			if ip, err = resolver.ResolveIPWithResolver(ctx, c.host, c.r); err != nil {
				return nil, fmt.Errorf("use default dns resolve failed: %w", err)
			}
			c.host = ip.String()
		}
	}

	network := "udp"
	if strings.HasPrefix(c.Client.Net, "tcp") {
		network = "tcp"
	}

	options := []dialer.Option{}
	if c.iface != "" {
		options = append(options, dialer.WithInterface(c.iface))
	}

	var conn net.Conn
	if c.proxyAdapter != "" {
		conn, err = dialContextWithProxyAdapter(ctx, c.proxyAdapter, network, ip, c.port, options...)
		if err == errProxyNotFound {
			options = append(options[:0], dialer.WithInterface(c.proxyAdapter), dialer.WithRoutingMark(0))
			conn, err = dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), c.port), options...)
		}
	} else {
		conn, err = dialer.DialContext(ctx, network, net.JoinHostPort(ip.String(), c.port), options...)
	}

	if err != nil {
		return nil, err
	}
	defer func() {
		_ = conn.Close()
	}()

	// miekg/dns ExchangeContext doesn't respond to context cancel.
	// this is a workaround
	type result struct {
		msg *D.Msg
		err error
	}
	ch := make(chan result, 1)
	go func() {
		if strings.HasSuffix(c.Client.Net, "tls") {
			conn = tls.Client(conn, c.Client.TLSConfig)
		}

		msg, _, err := c.Client.ExchangeWithConn(m, &D.Conn{
			Conn:         conn,
			UDPSize:      c.Client.UDPSize,
			TsigSecret:   c.Client.TsigSecret,
			TsigProvider: c.Client.TsigProvider,
		})

		ch <- result{msg, err}
	}()

	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case ret := <-ch:
		return ret.msg, ret.err
	}
}
