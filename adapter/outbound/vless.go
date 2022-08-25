package outbound

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"

	"golang.org/x/net/http2"

	"github.com/Dreamacro/clash/component/dialer"
	"github.com/Dreamacro/clash/component/resolver"
	C "github.com/Dreamacro/clash/constant"
	"github.com/Dreamacro/clash/transport/gun"
	"github.com/Dreamacro/clash/transport/socks5"
	"github.com/Dreamacro/clash/transport/vless"
	"github.com/Dreamacro/clash/transport/vmess"
)

const (
	// max packet length
	maxLength = 1024 << 3
)

type Vless struct {
	*Base
	client *vless.Client
	option *VlessOption

	// for gun mux
	gunTLSConfig *tls.Config
	gunConfig    *gun.Config
	transport    *http2.Transport
}

type VlessOption struct {
	BasicOption
	Name           string            `proxy:"name"`
	Server         string            `proxy:"server"`
	Port           int               `proxy:"port"`
	UUID           string            `proxy:"uuid"`
	Flow           string            `proxy:"flow,omitempty"`
	FlowShow       bool              `proxy:"flow-show,omitempty"`
	UDP            bool              `proxy:"udp,omitempty"`
	Network        string            `proxy:"network,omitempty"`
	HTTPOpts       HTTPOptions       `proxy:"http-opts,omitempty"`
	HTTP2Opts      HTTP2Options      `proxy:"h2-opts,omitempty"`
	GrpcOpts       GrpcOptions       `proxy:"grpc-opts,omitempty"`
	WSOpts         WSOptions         `proxy:"ws-opts,omitempty"`
	WSPath         string            `proxy:"ws-path,omitempty"`
	WSHeaders      map[string]string `proxy:"ws-headers,omitempty"`
	SkipCertVerify bool              `proxy:"skip-cert-verify,omitempty"`
	ServerName     string            `proxy:"servername,omitempty"`
}

// StreamConn implements C.ProxyAdapter
func (v *Vless) StreamConn(c net.Conn, metadata *C.Metadata) (net.Conn, error) {
	var err error
	switch v.option.Network {
	case "ws":
		if v.option.WSOpts.Path == "" {
			v.option.WSOpts.Path = v.option.WSPath
		}
		if len(v.option.WSOpts.Headers) == 0 {
			v.option.WSOpts.Headers = v.option.WSHeaders
		}

		host, port, _ := net.SplitHostPort(v.addr)
		wsOpts := &vmess.WebsocketConfig{
			Host:                host,
			Port:                port,
			Path:                v.option.WSOpts.Path,
			MaxEarlyData:        v.option.WSOpts.MaxEarlyData,
			EarlyDataHeaderName: v.option.WSOpts.EarlyDataHeaderName,
		}

		if len(v.option.WSOpts.Headers) != 0 {
			header := http.Header{}
			for key, value := range v.option.WSOpts.Headers {
				header.Add(key, value)
			}
			wsOpts.Headers = header
		}

		wsOpts.TLS = true
		wsOpts.TLSConfig = &tls.Config{
			MinVersion:         tls.VersionTLS12,
			ServerName:         host,
			InsecureSkipVerify: v.option.SkipCertVerify,
			NextProtos:         []string{"http/1.1"},
		}
		if v.option.ServerName != "" {
			wsOpts.TLSConfig.ServerName = v.option.ServerName
		} else if host := wsOpts.Headers.Get("Host"); host != "" {
			wsOpts.TLSConfig.ServerName = host
		}

		c, err = vmess.StreamWebsocketConn(c, wsOpts)
	case "http":
		// readability first, so just copy default TLS logic
		c, err = v.streamTLSOrXTLSConn(c, false)
		if err != nil {
			return nil, err
		}

		host, _, _ := net.SplitHostPort(v.addr)
		httpOpts := &vmess.HTTPConfig{
			Host:    host,
			Method:  v.option.HTTPOpts.Method,
			Path:    v.option.HTTPOpts.Path,
			Headers: v.option.HTTPOpts.Headers,
		}

		c = vmess.StreamHTTPConn(c, httpOpts)
	case "h2":
		c, err = v.streamTLSOrXTLSConn(c, true)
		if err != nil {
			return nil, err
		}

		h2Opts := &vmess.H2Config{
			Hosts: v.option.HTTP2Opts.Host,
			Path:  v.option.HTTP2Opts.Path,
		}

		c, err = vmess.StreamH2Conn(c, h2Opts)
	case "grpc":
		if v.isXTLSEnabled() {
			c, err = gun.StreamGunWithXTLSConn(c, v.gunTLSConfig, v.gunConfig)
		} else {
			c, err = gun.StreamGunWithConn(c, v.gunTLSConfig, v.gunConfig)
		}
	default:
		// default tcp network
		// handle TLS And XTLS
		c, err = v.streamTLSOrXTLSConn(c, false)
	}

	if err != nil {
		return nil, err
	}

	return v.client.StreamConn(c, parseVlessAddr(metadata))
}

// StreamPacketConn implements C.ProxyAdapter
func (v *Vless) StreamPacketConn(c net.Conn, metadata *C.Metadata) (net.Conn, error) {
	// vmess use stream-oriented udp with a special address, so we need a net.UDPAddr
	if !metadata.Resolved() {
		ip, err := resolver.ResolveFirstIP(metadata.Host)
		if err != nil {
			return nil, errors.New("can't resolve ip")
		}
		metadata.DstIP = ip
	}

	var err error
	c, err = v.StreamConn(c, metadata)
	if err != nil {
		return nil, fmt.Errorf("new vmess client error: %v", err)
	}

	return WrapConn(&vlessPacketConn{Conn: c, rAddr: metadata.UDPAddr()}), nil
}

func (v *Vless) streamTLSOrXTLSConn(conn net.Conn, isH2 bool) (net.Conn, error) {
	host, _, _ := net.SplitHostPort(v.addr)

	if v.isXTLSEnabled() {
		xtlsOpts := vless.XTLSConfig{
			Host:           host,
			SkipCertVerify: v.option.SkipCertVerify,
		}

		if isH2 {
			xtlsOpts.NextProtos = []string{"h2"}
		}

		if v.option.ServerName != "" {
			xtlsOpts.Host = v.option.ServerName
		}

		return vless.StreamXTLSConn(conn, &xtlsOpts)

	} else {
		tlsOpts := vmess.TLSConfig{
			Host:           host,
			SkipCertVerify: v.option.SkipCertVerify,
		}

		if isH2 {
			tlsOpts.NextProtos = []string{"h2"}
		}

		if v.option.ServerName != "" {
			tlsOpts.Host = v.option.ServerName
		}

		return vmess.StreamTLSConn(conn, &tlsOpts)
	}
}

func (v *Vless) isXTLSEnabled() bool {
	return v.client.Addons != nil
}

// DialContext implements C.ProxyAdapter
func (v *Vless) DialContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (_ C.Conn, err error) {
	// gun transport
	if v.transport != nil && len(opts) == 0 {
		c, err := gun.StreamGunWithTransport(v.transport, v.gunConfig)
		if err != nil {
			return nil, err
		}
		defer safeConnClose(c, err)

		c, err = v.client.StreamConn(c, parseVlessAddr(metadata))
		if err != nil {
			return nil, err
		}

		return NewConn(c, v), nil
	}

	c, err := dialer.DialContext(ctx, "tcp", v.addr, v.Base.DialOptions(opts...)...)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %s", v.addr, err.Error())
	}
	tcpKeepAlive(c)
	defer safeConnClose(c, err)

	c, err = v.StreamConn(c, metadata)
	return NewConn(c, v), err
}

// ListenPacketContext implements C.ProxyAdapter
func (v *Vless) ListenPacketContext(ctx context.Context, metadata *C.Metadata, opts ...dialer.Option) (_ C.PacketConn, err error) {
	var c net.Conn
	// gun transport
	if v.transport != nil && len(opts) == 0 {
		// vless use stream-oriented udp with a special address, so we need a net.UDPAddr
		if !metadata.Resolved() {
			ip, err := resolver.ResolveFirstIP(metadata.Host)
			if err != nil {
				return nil, errors.New("can't resolve ip")
			}
			metadata.DstIP = ip
		}

		c, err = gun.StreamGunWithTransport(v.transport, v.gunConfig)
		if err != nil {
			return nil, err
		}
		defer safeConnClose(c, err)

		c, err = v.client.StreamConn(c, parseVlessAddr(metadata))
		if err != nil {
			return nil, fmt.Errorf("new vless client error: %v", err)
		}

		return NewPacketConn(&vlessPacketConn{Conn: c, rAddr: metadata.UDPAddr()}, v), nil
	}

	c, err = dialer.DialContext(ctx, "tcp", v.addr, v.Base.DialOptions(opts...)...)
	if err != nil {
		return nil, fmt.Errorf("%s connect error: %s", v.addr, err.Error())
	}

	tcpKeepAlive(c)
	defer safeConnClose(c, err)

	c, err = v.StreamPacketConn(c, metadata)
	if err != nil {
		return nil, fmt.Errorf("new vless client error: %v", err)
	}

	return NewPacketConn(c.(net.PacketConn), v), nil
}

func parseVlessAddr(metadata *C.Metadata) *vless.DstAddr {
	var addrType byte
	var addr []byte
	switch metadata.AddrType() {
	case socks5.AtypIPv4:
		addrType = vless.AtypIPv4
		addr = make([]byte, net.IPv4len)
		copy(addr[:], metadata.DstIP.AsSlice())
	case socks5.AtypIPv6:
		addrType = vless.AtypIPv6
		addr = make([]byte, net.IPv6len)
		copy(addr[:], metadata.DstIP.AsSlice())
	case socks5.AtypDomainName:
		addrType = vless.AtypDomainName
		addr = make([]byte, len(metadata.Host)+1)
		addr[0] = byte(len(metadata.Host))
		copy(addr[1:], metadata.Host)
	}

	port, _ := strconv.ParseUint(metadata.DstPort, 10, 16)
	return &vless.DstAddr{
		UDP:      metadata.NetWork == C.UDP,
		AddrType: addrType,
		Addr:     addr,
		Port:     uint(port),
	}
}

type vlessPacketConn struct {
	net.Conn
	rAddr  net.Addr
	cache  [2]byte
	remain int
	mux    sync.Mutex
}

func (vc *vlessPacketConn) WriteTo(b []byte, _ net.Addr) (int, error) {
	total := len(b)
	if total == 0 {
		return 0, nil
	}

	if total < maxLength {
		return vc.writePacket(b)
	}

	offset := 0
	for {
		cursor := offset + maxLength
		if cursor > total {
			cursor = total
		}

		n, err := vc.writePacket(b[offset:cursor])
		if err != nil {
			return offset + n, err
		}

		offset = cursor
		if offset == total {
			break
		}
	}

	return total, nil
}

func (vc *vlessPacketConn) ReadFrom(b []byte) (int, net.Addr, error) {
	vc.mux.Lock()
	defer vc.mux.Unlock()

	if vc.remain != 0 {
		length := len(b)
		if length > vc.remain {
			length = vc.remain
		}

		n, err := vc.Conn.Read(b[:length])
		if err != nil {
			return 0, vc.rAddr, err
		}

		vc.remain -= n

		return n, vc.rAddr, nil
	}

	if _, err := vc.Conn.Read(b[:2]); err != nil {
		return 0, vc.rAddr, err
	}

	total := int(binary.BigEndian.Uint16(b[:2]))
	if total == 0 {
		return 0, vc.rAddr, nil
	}

	length := len(b)
	if length > total {
		length = total
	}

	if _, err := io.ReadFull(vc.Conn, b[:length]); err != nil {
		return 0, vc.rAddr, errors.New("read packet error")
	}

	vc.remain = total - length

	return length, vc.rAddr, nil
}

func (vc *vlessPacketConn) writePacket(payload []byte) (int, error) {
	binary.BigEndian.PutUint16(vc.cache[:], uint16(len(payload)))

	if _, err := vc.Conn.Write(vc.cache[:]); err != nil {
		return 0, err
	}

	return vc.Conn.Write(payload)
}

func NewVless(option VlessOption) (*Vless, error) {
	var addons *vless.Addons
	if option.Network != "ws" && len(option.Flow) >= 16 {
		option.Flow = option.Flow[:16]
		switch option.Flow {
		case vless.XRO, vless.XRD, vless.XRS:
			addons = &vless.Addons{
				Flow: option.Flow,
			}
		default:
			return nil, fmt.Errorf("unsupported xtls flow type: %s", option.Flow)
		}
	}

	client, err := vless.NewClient(option.UUID, addons, option.FlowShow)
	if err != nil {
		return nil, err
	}

	v := &Vless{
		Base: &Base{
			name:  option.Name,
			addr:  net.JoinHostPort(option.Server, strconv.Itoa(option.Port)),
			tp:    C.Vless,
			udp:   option.UDP,
			iface: option.Interface,
		},
		client: client,
		option: &option,
	}

	switch option.Network {
	case "h2":
		if len(option.HTTP2Opts.Host) == 0 {
			option.HTTP2Opts.Host = append(option.HTTP2Opts.Host, "www.example.com")
		}
	case "grpc":
		dialFn := func(network, addr string) (net.Conn, error) {
			c, err := dialer.DialContext(context.Background(), "tcp", v.addr, v.Base.DialOptions()...)
			if err != nil {
				return nil, fmt.Errorf("%s connect error: %s", v.addr, err.Error())
			}
			tcpKeepAlive(c)
			return c, nil
		}

		gunConfig := &gun.Config{
			ServiceName: v.option.GrpcOpts.GrpcServiceName,
			Host:        v.option.ServerName,
		}
		tlsConfig := &tls.Config{
			InsecureSkipVerify: v.option.SkipCertVerify,
			ServerName:         v.option.ServerName,
		}

		if v.option.ServerName == "" {
			host, _, _ := net.SplitHostPort(v.addr)
			tlsConfig.ServerName = host
			gunConfig.Host = host
		}

		v.gunTLSConfig = tlsConfig
		v.gunConfig = gunConfig
		if v.isXTLSEnabled() {
			v.transport = gun.NewHTTP2XTLSClient(dialFn, tlsConfig)
		} else {
			v.transport = gun.NewHTTP2Client(dialFn, tlsConfig)
		}
	}

	return v, nil
}
