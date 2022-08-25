//go:build !linux

package tun

import (
	"fmt"
	"os"
	"runtime"

	"golang.zx2c4.com/wireguard/tun"

	"github.com/Dreamacro/clash/listener/tun/device"
	"github.com/Dreamacro/clash/listener/tun/device/iobased"
)

type TUN struct {
	*iobased.Endpoint

	nt     *tun.NativeTun
	mtu    uint32
	name   string
	offset int

	cache []byte
}

func Open(name string, mtu uint32) (_ device.Device, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("open tun: %v", r)
		}
	}()

	t := &TUN{
		name:   name,
		mtu:    mtu,
		offset: offset,
	}

	forcedMTU := defaultMTU
	if t.mtu > 0 {
		forcedMTU = int(t.mtu)
	}

	nt, err := newDevice(t.name, forcedMTU) // forcedMTU do not work on wintun, need to be setting by other way

	// retry if abnormal exit at last time on Windows
	if err != nil && runtime.GOOS == "windows" && os.IsExist(err) {
		nt, err = newDevice(t.name, forcedMTU)
	}

	if err != nil {
		return nil, fmt.Errorf("create tun: %w", err)
	}

	t.nt = nt.(*tun.NativeTun)

	tunMTU, err := nt.MTU()
	if err != nil {
		return nil, fmt.Errorf("get mtu: %w", err)
	}

	if t.mtu == 0 {
		t.mtu = uint32(tunMTU)
	}

	if t.offset > 0 {
		t.cache = make([]byte, int(t.mtu)+t.offset)
	}

	return t, nil
}

func (t *TUN) Read(packet []byte) (int, error) {
	if t.offset == 0 {
		return t.nt.Read(packet, t.offset)
	}

	n, err := t.nt.Read(t.cache, t.offset)

	copy(packet, t.cache[t.offset:t.offset+n])

	return n, err
}

func (t *TUN) Write(packet []byte) (int, error) {
	if t.offset == 0 {
		return t.nt.Write(packet, t.offset)
	}

	packet = append(t.cache[:t.offset], packet...)

	n, err := t.nt.Write(packet, t.offset)
	if n < t.offset {
		return 0, err
	}
	return n - t.offset, err
}

func (t *TUN) Close() error {
	defer func(ep *iobased.Endpoint) {
		if ep != nil {
			ep.Close()
		}
	}(t.Endpoint)
	return t.nt.Close()
}

func (t *TUN) Name() string {
	name, _ := t.nt.Name()
	return name
}

func (t *TUN) MTU() uint32 {
	return t.mtu
}

func (t *TUN) UseEndpoint() error {
	ep, err := iobased.New(t, t.mtu, t.offset)
	if err != nil {
		return fmt.Errorf("create endpoint: %w", err)
	}
	t.Endpoint = ep
	return nil
}

func (t *TUN) UseIOBased() error {
	return nil
}
