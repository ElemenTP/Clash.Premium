package proxy

import (
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/netip"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/exp/slices"

	"github.com/Dreamacro/clash/adapter/inbound"
	"github.com/Dreamacro/clash/adapter/outbound"
	"github.com/Dreamacro/clash/common/cert"
	"github.com/Dreamacro/clash/component/ebpf"
	"github.com/Dreamacro/clash/config"
	C "github.com/Dreamacro/clash/constant"
	"github.com/Dreamacro/clash/listener/autoredir"
	"github.com/Dreamacro/clash/listener/http"
	"github.com/Dreamacro/clash/listener/mitm"
	"github.com/Dreamacro/clash/listener/mixed"
	"github.com/Dreamacro/clash/listener/redir"
	"github.com/Dreamacro/clash/listener/socks"
	"github.com/Dreamacro/clash/listener/tproxy"
	"github.com/Dreamacro/clash/listener/tun"
	"github.com/Dreamacro/clash/listener/tun/ipstack"
	"github.com/Dreamacro/clash/listener/tun/ipstack/commons"
	"github.com/Dreamacro/clash/log"
	rewrites "github.com/Dreamacro/clash/rewrite"
	"github.com/Dreamacro/clash/tunnel"
)

var (
	allowLan    = false
	bindAddress = "*"
	lastTunConf *config.Tun

	socksListener     *socks.Listener
	socksUDPListener  *socks.UDPListener
	httpListener      *http.Listener
	redirListener     *redir.Listener
	redirUDPListener  *tproxy.UDPListener
	tproxyListener    *tproxy.Listener
	tproxyUDPListener *tproxy.UDPListener
	mixedListener     *mixed.Listener
	mixedUDPLister    *socks.UDPListener
	tunStackListener  ipstack.Stack
	mitmListener      *mitm.Listener
	tcProgram         *ebpf.TcEBpfProgram
	autoRedirListener *autoredir.Listener
	autoRedirProgram  *ebpf.TcEBpfProgram

	// lock for recreate function
	socksMux     sync.Mutex
	httpMux      sync.Mutex
	redirMux     sync.Mutex
	tproxyMux    sync.Mutex
	mixedMux     sync.Mutex
	tunMux       sync.Mutex
	mitmMux      sync.Mutex
	tcMux        sync.Mutex
	autoRedirMux sync.Mutex
)

type Ports struct {
	Port       int `json:"port"`
	SocksPort  int `json:"socks-port"`
	RedirPort  int `json:"redir-port"`
	TProxyPort int `json:"tproxy-port"`
	MixedPort  int `json:"mixed-port"`
	MitmPort   int `json:"mitm-port"`
}

func GetTunConf() config.Tun {
	if lastTunConf == nil {
		addrPort := C.DNSAddrPort{
			AddrPort: netip.MustParseAddrPort("0.0.0.0:53"),
		}
		return config.Tun{
			Enable: false,
			Stack:  C.TunGvisor,
			DNSHijack: []C.DNSUrl{ // default hijack all dns query
				{
					Network:  "udp",
					AddrPort: addrPort,
				},
				{
					Network:  "tcp",
					AddrPort: addrPort,
				},
			},
			AutoRoute:           true,
			AutoDetectInterface: false,
		}
	}
	return *lastTunConf
}

func AllowLan() bool {
	return allowLan
}

func BindAddress() string {
	return bindAddress
}

func SetAllowLan(al bool) {
	allowLan = al
}

func SetBindAddress(host string) {
	bindAddress = host
}

func ReCreateHTTP(port int, tcpIn chan<- C.ConnContext) {
	httpMux.Lock()
	defer httpMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start HTTP server error: %s", err.Error())
		}
	}()

	addr := genAddr(bindAddress, port, allowLan)

	if httpListener != nil {
		if httpListener.RawAddress() == addr {
			return
		}
		httpListener.Close()
		httpListener = nil
	}

	if portIsZero(addr) {
		return
	}

	httpListener, err = http.New(addr, tcpIn)
	if err != nil {
		return
	}

	log.Infoln("HTTP proxy listening at: %s", httpListener.Address())
}

func ReCreateSocks(port int, tcpIn chan<- C.ConnContext, udpIn chan<- *inbound.PacketAdapter) {
	socksMux.Lock()
	defer socksMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start SOCKS server error: %s", err.Error())
		}
	}()

	addr := genAddr(bindAddress, port, allowLan)

	shouldTCPIgnore := false
	shouldUDPIgnore := false

	if socksListener != nil {
		if socksListener.RawAddress() != addr {
			socksListener.Close()
			socksListener = nil
		} else {
			shouldTCPIgnore = true
		}
	}

	if socksUDPListener != nil {
		if socksUDPListener.RawAddress() != addr {
			socksUDPListener.Close()
			socksUDPListener = nil
		} else {
			shouldUDPIgnore = true
		}
	}

	if shouldTCPIgnore && shouldUDPIgnore {
		return
	}

	if portIsZero(addr) {
		return
	}

	tcpListener, err := socks.New(addr, tcpIn)
	if err != nil {
		return
	}

	udpListener, err := socks.NewUDP(addr, udpIn)
	if err != nil {
		tcpListener.Close()
		return
	}

	socksListener = tcpListener
	socksUDPListener = udpListener

	log.Infoln("SOCKS proxy listening at: %s", socksListener.Address())
}

func ReCreateRedir(port int, tcpIn chan<- C.ConnContext, udpIn chan<- *inbound.PacketAdapter) {
	redirMux.Lock()
	defer redirMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start Redir server error: %s", err.Error())
		}
	}()

	addr := genAddr(bindAddress, port, allowLan)

	if redirListener != nil {
		if redirListener.RawAddress() == addr {
			return
		}
		redirListener.Close()
		redirListener = nil
	}

	if redirUDPListener != nil {
		if redirUDPListener.RawAddress() == addr {
			return
		}
		redirUDPListener.Close()
		redirUDPListener = nil
	}

	if portIsZero(addr) {
		return
	}

	redirListener, err = redir.New(addr, tcpIn)
	if err != nil {
		return
	}

	redirUDPListener, err = tproxy.NewUDP(addr, udpIn)
	if err != nil {
		log.Warnln("Failed to start Redir UDP Listener: %s", err)
	}

	log.Infoln("Redirect proxy listening at: %s", redirListener.Address())
}

func ReCreateTProxy(port int, tcpIn chan<- C.ConnContext, udpIn chan<- *inbound.PacketAdapter) {
	tproxyMux.Lock()
	defer tproxyMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start TProxy server error: %s", err.Error())
		}
	}()

	addr := genAddr(bindAddress, port, allowLan)

	if tproxyListener != nil {
		if tproxyListener.RawAddress() == addr {
			return
		}
		tproxyListener.Close()
		tproxyListener = nil
	}

	if tproxyUDPListener != nil {
		if tproxyUDPListener.RawAddress() == addr {
			return
		}
		tproxyUDPListener.Close()
		tproxyUDPListener = nil
	}

	if portIsZero(addr) {
		return
	}

	tproxyListener, err = tproxy.New(addr, tcpIn)
	if err != nil {
		return
	}

	tproxyUDPListener, err = tproxy.NewUDP(addr, udpIn)
	if err != nil {
		log.Warnln("Failed to start TProxy UDP Listener: %s", err)
	}

	log.Infoln("TProxy server listening at: %s", tproxyListener.Address())
}

func ReCreateMixed(port int, tcpIn chan<- C.ConnContext, udpIn chan<- *inbound.PacketAdapter) {
	mixedMux.Lock()
	defer mixedMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start Mixed(http+socks) server error: %s", err.Error())
		}
	}()

	addr := genAddr(bindAddress, port, allowLan)

	shouldTCPIgnore := false
	shouldUDPIgnore := false

	if mixedListener != nil {
		if mixedListener.RawAddress() != addr {
			mixedListener.Close()
			mixedListener = nil
		} else {
			shouldTCPIgnore = true
		}
	}
	if mixedUDPLister != nil {
		if mixedUDPLister.RawAddress() != addr {
			mixedUDPLister.Close()
			mixedUDPLister = nil
		} else {
			shouldUDPIgnore = true
		}
	}

	if shouldTCPIgnore && shouldUDPIgnore {
		return
	}

	if portIsZero(addr) {
		return
	}

	mixedListener, err = mixed.New(addr, tcpIn)
	if err != nil {
		return
	}

	mixedUDPLister, err = socks.NewUDP(addr, udpIn)
	if err != nil {
		mixedListener.Close()
		return
	}

	log.Infoln("Mixed(http+socks) proxy listening at: %s", mixedListener.Address())
}

func ReCreateTun(tunConf *config.Tun, tcpIn chan<- C.ConnContext, udpIn chan<- *inbound.PacketAdapter) {
	tunMux.Lock()
	defer tunMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start TUN listening error: %s", err.Error())
		}
	}()

	tunConf.DNSHijack = C.RemoveDuplicateDNSUrl(tunConf.DNSHijack)

	if tunStackListener != nil {
		if !hasTunConfigChange(tunConf) {
			return
		}

		_ = tunStackListener.Close()
		tunStackListener = nil
	}

	lastTunConf = tunConf

	if !tunConf.Enable {
		return
	}

	callback := &tunChangeCallback{
		tunConf: *tunConf,
		tcpIn:   tcpIn,
		udpIn:   udpIn,
	}

	tunStackListener, err = tun.New(tunConf, tcpIn, udpIn, callback)
	if err != nil {
		return
	}
}

func ReCreateMitm(port int, tcpIn chan<- C.ConnContext) {
	mitmMux.Lock()
	defer mitmMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			log.Errorln("Start MITM server error: %s", err.Error())
		}
	}()

	addr := genAddr(bindAddress, port, allowLan)

	if mitmListener != nil {
		if mitmListener.RawAddress() == addr {
			return
		}
		_ = mitmListener.Close()
		mitmListener = nil
		tunnel.SetMitmOutbound(nil)
	}

	if portIsZero(addr) {
		return
	}

	if err = initCert(); err != nil {
		return
	}

	var (
		rootCACert tls.Certificate
		x509c      *x509.Certificate
		certOption *cert.Config
	)

	rootCACert, err = tls.LoadX509KeyPair(C.Path.RootCA(), C.Path.CAKey())
	if err != nil {
		return
	}

	privateKey := rootCACert.PrivateKey.(*rsa.PrivateKey)

	x509c, err = x509.ParseCertificate(rootCACert.Certificate[0])
	if err != nil {
		return
	}

	certOption, err = cert.NewConfig(
		x509c,
		privateKey,
	)
	if err != nil {
		return
	}

	certOption.SetValidity(time.Hour * 24 * 365 * 2) // 2 years
	certOption.SetOrganization("Clash ManInTheMiddle Proxy Services")

	opt := &mitm.Option{
		Addr:       addr,
		ApiHost:    "mitm.clash",
		CertConfig: certOption,
		Handler:    &rewrites.RewriteHandler{},
	}

	mitmListener, err = mitm.New(opt, tcpIn)
	if err != nil {
		return
	}

	tunnel.SetMitmOutbound(outbound.NewMitm(mitmListener.Address()))

	log.Infoln("Mitm proxy listening at: %s", mitmListener.Address())
}

func ReCreateRedirToTun(ifaceNames []string) {
	tcMux.Lock()
	defer tcMux.Unlock()

	nicArr := ifaceNames
	slices.Sort(nicArr)
	nicArr = slices.Compact(nicArr)

	if tcProgram != nil {
		tcProgram.Close()
		tcProgram = nil
	}

	if len(nicArr) == 0 {
		return
	}

	if lastTunConf == nil || !lastTunConf.Enable {
		return
	}

	program, err := ebpf.NewTcEBpfProgram(nicArr, lastTunConf.Device)
	if err != nil {
		log.Errorln("Attached tc ebpf program error: %v", err)
		return
	}
	tcProgram = program

	log.Infoln("Attached tc ebpf program to interfaces %v", tcProgram.RawNICs())
}

func ReCreateAutoRedir(ifaceNames []string, tcpIn chan<- C.ConnContext, _ chan<- *inbound.PacketAdapter) {
	autoRedirMux.Lock()
	defer autoRedirMux.Unlock()

	var err error
	defer func() {
		if err != nil {
			if redirListener != nil {
				_ = redirListener.Close()
				redirListener = nil
			}
			if autoRedirProgram != nil {
				autoRedirProgram.Close()
				autoRedirProgram = nil
			}
			log.Errorln("Start auto redirect server error: %s", err.Error())
		}
	}()

	nicArr := ifaceNames
	slices.Sort(nicArr)
	nicArr = slices.Compact(nicArr)

	if redirListener != nil && autoRedirProgram != nil {
		_ = redirListener.Close()
		autoRedirProgram.Close()
		redirListener = nil
		autoRedirProgram = nil
	}

	if len(nicArr) == 0 {
		return
	}

	defaultRouteInterfaceName, err := commons.GetAutoDetectInterface()
	if err != nil {
		return
	}

	addr := genAddr("*", C.TcpAutoRedirPort, true)

	autoRedirListener, err = autoredir.New(addr, tcpIn)
	if err != nil {
		return
	}

	autoRedirProgram, err = ebpf.NewRedirEBpfProgram(nicArr, autoRedirListener.TCPAddr().Port(), defaultRouteInterfaceName)
	if err != nil {
		return
	}

	autoRedirListener.SetLookupFunc(autoRedirProgram.Lookup)

	log.Infoln("Auto redirect proxy listening at: %s, attached tc ebpf program to interfaces %v", autoRedirListener.Address(), autoRedirProgram.RawNICs())
}

// GetPorts return the ports of proxy servers
func GetPorts() *Ports {
	ports := &Ports{}

	if httpListener != nil {
		_, portStr, _ := net.SplitHostPort(httpListener.Address())
		port, _ := strconv.Atoi(portStr)
		ports.Port = port
	}

	if socksListener != nil {
		_, portStr, _ := net.SplitHostPort(socksListener.Address())
		port, _ := strconv.Atoi(portStr)
		ports.SocksPort = port
	}

	if redirListener != nil {
		_, portStr, _ := net.SplitHostPort(redirListener.Address())
		port, _ := strconv.Atoi(portStr)
		ports.RedirPort = port
	}

	if tproxyListener != nil {
		_, portStr, _ := net.SplitHostPort(tproxyListener.Address())
		port, _ := strconv.Atoi(portStr)
		ports.TProxyPort = port
	}

	if mixedListener != nil {
		_, portStr, _ := net.SplitHostPort(mixedListener.Address())
		port, _ := strconv.Atoi(portStr)
		ports.MixedPort = port
	}

	if mitmListener != nil {
		_, portStr, _ := net.SplitHostPort(mitmListener.Address())
		port, _ := strconv.Atoi(portStr)
		ports.MitmPort = port
	}

	return ports
}

func portIsZero(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if port == "0" || port == "" || err != nil {
		return true
	}
	return false
}

func genAddr(host string, port int, allowLan bool) string {
	if allowLan {
		if host == "*" {
			return fmt.Sprintf(":%d", port)
		}
		return fmt.Sprintf("%s:%d", host, port)
	}

	return fmt.Sprintf("127.0.0.1:%d", port)
}

func hasTunConfigChange(tunConf *config.Tun) bool {
	if lastTunConf == nil {
		return true
	}

	if len(lastTunConf.DNSHijack) != len(tunConf.DNSHijack) {
		return true
	}

	for i, dns := range tunConf.DNSHijack {
		if dns != lastTunConf.DNSHijack[i] {
			return true
		}
	}

	if lastTunConf.Enable != tunConf.Enable ||
		lastTunConf.Device != tunConf.Device ||
		lastTunConf.Stack != tunConf.Stack ||
		lastTunConf.AutoRoute != tunConf.AutoRoute ||
		lastTunConf.AutoDetectInterface != tunConf.AutoDetectInterface {
		return true
	}

	if (lastTunConf.TunAddressPrefix != nil && tunConf.TunAddressPrefix == nil) || (lastTunConf.TunAddressPrefix == nil && tunConf.TunAddressPrefix != nil) {
		return true
	}

	if lastTunConf.TunAddressPrefix != nil && tunConf.TunAddressPrefix != nil && *lastTunConf.TunAddressPrefix != *tunConf.TunAddressPrefix {
		return true
	}

	return false
}

type tunChangeCallback struct {
	tunConf config.Tun
	tcpIn   chan<- C.ConnContext
	udpIn   chan<- *inbound.PacketAdapter
}

func (t *tunChangeCallback) Pause() {
	conf := t.tunConf
	conf.Enable = false
	ReCreateTun(&conf, t.tcpIn, t.udpIn)
	ReCreateRedirToTun([]string{})
}

func (t *tunChangeCallback) Resume() {
	conf := t.tunConf
	conf.Enable = true
	ReCreateTun(&conf, t.tcpIn, t.udpIn)
	ReCreateRedirToTun(conf.RedirectToTun)
}

func initCert() error {
	if _, err := os.Stat(C.Path.RootCA()); os.IsNotExist(err) {
		log.Infoln("Can't find mitm_ca.crt, start generate")
		err = cert.GenerateAndSave(C.Path.RootCA(), C.Path.CAKey())
		if err != nil {
			return err
		}
		log.Infoln("Generated CA private key and CA certificate finish")
	}

	return nil
}

func Cleanup() {
	if tcProgram != nil {
		tcProgram.Close()
	}
	if autoRedirProgram != nil {
		autoRedirProgram.Close()
	}
	if tunStackListener != nil {
		_ = tunStackListener.Close()
	}
}
