package tun

import (
	"fmt"
	"net/netip"
	"net/url"
	"runtime"
	"strings"

	"github.com/Dreamacro/clash/adapter/inbound"
	"github.com/Dreamacro/clash/common/cmd"
	"github.com/Dreamacro/clash/config"
	C "github.com/Dreamacro/clash/constant"
	"github.com/Dreamacro/clash/listener/tun/device"
	"github.com/Dreamacro/clash/listener/tun/device/fdbased"
	"github.com/Dreamacro/clash/listener/tun/device/tun"
	"github.com/Dreamacro/clash/listener/tun/ipstack"
	"github.com/Dreamacro/clash/listener/tun/ipstack/commons"
	"github.com/Dreamacro/clash/listener/tun/ipstack/gvisor"
	"github.com/Dreamacro/clash/listener/tun/ipstack/gvisor/option"
	"github.com/Dreamacro/clash/listener/tun/ipstack/system"
	"github.com/Dreamacro/clash/log"
)

// New TunAdapter
func New(tunConf *config.Tun, tcpIn chan<- C.ConnContext, udpIn chan<- *inbound.PacketAdapter, tunChangeCallback C.TUNChangeCallback) (ipstack.Stack, error) {
	var (
		tunAddress = netip.Prefix{}
		devName    = tunConf.Device
		stackType  = tunConf.Stack
		autoRoute  = tunConf.AutoRoute
		mtu        = 9000

		tunDevice device.Device
		tunStack  ipstack.Stack

		err error
	)

	defer func() {
		if err != nil && tunDevice != nil {
			_ = tunDevice.Close()
		}
	}()

	if devName == "" {
		devName = generateDeviceName()
	}

	if tunConf.TunAddressPrefix != nil {
		tunAddress = *tunConf.TunAddressPrefix
	}

	if !tunAddress.IsValid() || !tunAddress.Addr().Is4() {
		tunAddress = netip.MustParsePrefix("198.18.0.1/16")
	}

	// open tun device
	tunDevice, err = parseDevice(devName, uint32(mtu))
	if err != nil {
		return nil, fmt.Errorf("can't open tun: %w", err)
	}

	devName = tunDevice.Name()

	// new ip stack
	switch stackType {
	case C.TunGvisor:
		err = tunDevice.UseEndpoint()
		if err != nil {
			return nil, fmt.Errorf("can't attach endpoint to tun: %w", err)
		}

		tunStack, err = gvisor.New(tunDevice, tunConf.DNSHijack, tunAddress, tcpIn, udpIn, option.WithDefault())

		if err != nil {
			return nil, fmt.Errorf("can't New gvisor stack: %w", err)
		}
	case C.TunSystem:
		err = tunDevice.UseIOBased()
		if err != nil {
			return nil, fmt.Errorf("can't New system stack: %w", err)
		}

		tunStack, err = system.New(tunDevice, tunConf.DNSHijack, tunAddress, tcpIn, udpIn)
		if err != nil {
			return nil, fmt.Errorf("can't New system stack: %w", err)
		}
	default:
		// never happen
	}

	// setting address and routing
	err = commons.ConfigInterfaceAddress(tunDevice, tunAddress, mtu, autoRoute)
	if err != nil {
		return nil, fmt.Errorf("setting interface address and routing failed: %w", err)
	}

	if tunConf.AutoDetectInterface {
		commons.SetTunChangeCallback(tunChangeCallback)
		go commons.StartDefaultInterfaceChangeMonitor()
	}

	tunConf.Device = devName
	setAtLatest(stackType, devName)

	log.Infoln("TUN stack listening at: %s(%s), mtu: %d, auto route: %v, ip stack: %s", tunDevice.Name(), tunAddress.Masked().Addr().Next().String(), mtu, autoRoute, stackType)
	return tunStack, nil
}

func generateDeviceName() string {
	switch runtime.GOOS {
	case "darwin":
		return tun.Driver + "://utun"
	case "windows":
		return tun.Driver + "://Clash"
	default:
		return tun.Driver + "://clash0"
	}
}

func parseDevice(s string, mtu uint32) (device.Device, error) {
	if !strings.Contains(s, "://") {
		s = fmt.Sprintf("%s://%s", tun.Driver /* default driver */, s)
	}

	u, err := url.Parse(s)
	if err != nil {
		return nil, err
	}

	name := u.Host
	driver := strings.ToLower(u.Scheme)

	switch driver {
	case fdbased.Driver:
		return fdbased.Open(name, mtu)
	case tun.Driver:
		return tun.Open(name, mtu)
	default:
		return nil, fmt.Errorf("unsupported driver: %s", driver)
	}
}

func setAtLatest(stackType C.TUNStack, devName string) {
	switch runtime.GOOS {
	case "darwin":
		// _, _ = cmd.ExecCmd("/usr/sbin/sysctl -w net.inet.ip.forwarding=1")
		// _, _ = cmd.ExecCmd("/usr/sbin/sysctl -w net.inet6.ip6.forwarding=1")
		_, _ = cmd.ExecCmd("/bin/launchctl limit maxfiles 10240 unlimited")
	case "windows":
		if stackType != C.TunSystem {
			return
		}
		_, _ = cmd.ExecCmd("ipconfig /renew")
	case "linux":
		_, _ = cmd.ExecCmd("sysctl -w net.ipv4.ip_forward=1")
		_, _ = cmd.ExecCmd("sysctl -w net.ipv4.conf.all.forwarding=1")
		_, _ = cmd.ExecCmd("sysctl -w net.ipv4.conf.all.accept_local=1")
		_, _ = cmd.ExecCmd("sysctl -w net.ipv4.conf.all.accept_redirects=1")
		_, _ = cmd.ExecCmd("sysctl -w net.ipv4.conf.all.rp_filter=0")
		_, _ = cmd.ExecCmd(fmt.Sprintf("sysctl -w net.ipv4.conf.%s.forwarding=1", devName))
		_, _ = cmd.ExecCmd(fmt.Sprintf("sysctl -w net.ipv4.conf.%s.accept_local=1", devName))
		_, _ = cmd.ExecCmd(fmt.Sprintf("sysctl -w net.ipv4.conf.%s.accept_redirects=1", devName))
		_, _ = cmd.ExecCmd(fmt.Sprintf("sysctl -w net.ipv4.conf.%s.rp_filter=0", devName))
		//_, _ = cmd.ExecCmd("iptables -t filter -P FORWARD ACCEPT")
	}
}
