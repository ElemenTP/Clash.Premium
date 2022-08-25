//go:build !windows

package fdbased

import (
	"fmt"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
	"gvisor.dev/gvisor/pkg/tcpip/stack"

	"github.com/Dreamacro/clash/listener/tun/device"
)

type FD struct {
	stack.LinkEndpoint

	fd  int
	mtu uint32

	file *os.File
}

func Open(name string, mtu uint32) (device.Device, error) {
	fd, err := strconv.Atoi(name)
	if err != nil {
		return nil, fmt.Errorf("cannot open fd: %s", name)
	}
	if mtu == 0 {
		mtu = defaultMTU
	}
	return open(fd, mtu)
}

func (f *FD) Type() string {
	return Driver
}

func (f *FD) Name() string {
	return strconv.Itoa(f.fd)
}

func (f *FD) MTU() uint32 {
	return f.mtu
}

func (f *FD) Close() error {
	err := unix.Close(f.fd)
	if f.file != nil {
		_ = f.file.Close()
	}
	return err
}

func (f *FD) UseEndpoint() error {
	return f.useEndpoint()
}

func (f *FD) UseIOBased() error {
	return f.useIOBased()
}

var _ device.Device = (*FD)(nil)
