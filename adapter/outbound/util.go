package outbound

import (
	"bytes"
	"net"
	"strconv"
	"time"

	"github.com/Dreamacro/clash/component/resolver"
	C "github.com/Dreamacro/clash/constant"
	"github.com/Dreamacro/clash/transport/socks5"
)

func tcpKeepAlive(c net.Conn) {
	if tcp, ok := c.(*net.TCPConn); ok {
		_ = tcp.SetKeepAlive(true)
		_ = tcp.SetKeepAlivePeriod(30 * time.Second)
		_ = tcp.SetLinger(0)
	}
}

func serializesSocksAddr(metadata *C.Metadata) []byte {
	var buf [][]byte
	addrType := metadata.AddrType()
	aType := uint8(addrType)
	p, _ := strconv.ParseUint(metadata.DstPort, 10, 16)
	port := []byte{uint8(p >> 8), uint8(p & 0xff)}
	switch addrType {
	case socks5.AtypDomainName:
		lenM := uint8(len(metadata.Host))
		host := []byte(metadata.Host)
		buf = [][]byte{{aType, lenM}, host, port}
	case socks5.AtypIPv4:
		host := metadata.DstIP.AsSlice()
		buf = [][]byte{{aType}, host, port}
	case socks5.AtypIPv6:
		host := metadata.DstIP.AsSlice()
		buf = [][]byte{{aType}, host, port}
	}
	return bytes.Join(buf, nil)
}

func resolveUDPAddr(network, address string) (*net.UDPAddr, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}

	ip, err := resolver.ResolveProxyServerHost(host)
	if err != nil {
		return nil, err
	}
	return net.ResolveUDPAddr(network, net.JoinHostPort(ip.String(), port))
}

func safeConnClose(c net.Conn, err error) {
	if err != nil && c != nil {
		_ = c.Close()
	}
}
