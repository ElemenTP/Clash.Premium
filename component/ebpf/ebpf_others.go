//go:build !linux

package ebpf

import (
	"fmt"
)

// NewTcEBpfProgram new ebpf tc program
func NewTcEBpfProgram(_ []string, _ string) (*TcEBpfProgram, error) {
	return nil, fmt.Errorf("system not supported")
}

// NewRedirEBpfProgram new ebpf redirect program
func NewRedirEBpfProgram(_ []string, _ uint16, _ string) (*TcEBpfProgram, error) {
	return nil, fmt.Errorf("system not supported")
}
