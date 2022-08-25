package main

import (
	"fmt"
	"testing"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/stretchr/testify/require"

	"github.com/Dreamacro/clash/adapter/outbound"
	C "github.com/Dreamacro/clash/constant"
)

func TestClash_VlessTLS(t *testing.T) {
	cfg := &container.Config{
		Image:        ImageVmess,
		ExposedPorts: defaultExposedPorts,
	}
	hostCfg := &container.HostConfig{
		PortBindings: defaultPortBindings,
		Binds: []string{
			fmt.Sprintf("%s:/etc/v2ray/config.json", C.Path.Resolve("vless-tls.json")),
			fmt.Sprintf("%s:/etc/ssl/v2ray/fullchain.pem", C.Path.Resolve("example.org.pem")),
			fmt.Sprintf("%s:/etc/ssl/v2ray/privkey.pem", C.Path.Resolve("example.org-key.pem")),
		},
	}

	id, err := startContainer(cfg, hostCfg, "vless-tls")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanContainer(id)
	})

	proxy, err := outbound.NewVless(outbound.VlessOption{
		Name:           "vless",
		Server:         localIP.String(),
		Port:           10002,
		UUID:           "b831381d-6324-4d53-ad4f-8cda48b30811",
		SkipCertVerify: true,
		ServerName:     "example.org",
		UDP:            true,
	})
	require.NoError(t, err)

	time.Sleep(waitTime)
	testSuit(t, proxy)
}

func TestClash_VlessXTLS(t *testing.T) {
	cfg := &container.Config{
		Image:        ImageXray,
		ExposedPorts: defaultExposedPorts,
	}
	hostCfg := &container.HostConfig{
		PortBindings: defaultPortBindings,
		Binds: []string{
			fmt.Sprintf("%s:/etc/xray/config.json", C.Path.Resolve("vless-xtls.json")),
			fmt.Sprintf("%s:/etc/ssl/v2ray/fullchain.pem", C.Path.Resolve("example.org.pem")),
			fmt.Sprintf("%s:/etc/ssl/v2ray/privkey.pem", C.Path.Resolve("example.org-key.pem")),
		},
	}

	id, err := startContainer(cfg, hostCfg, "vless-xtls")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanContainer(id)
	})

	proxy, err := outbound.NewVless(outbound.VlessOption{
		Name:           "vless",
		Server:         localIP.String(),
		Port:           10002,
		UUID:           "b831381d-6324-4d53-ad4f-8cda48b30811",
		SkipCertVerify: true,
		ServerName:     "example.org",
		UDP:            true,
		Flow:           "xtls-rprx-direct",
		FlowShow:       true,
	})
	require.NoError(t, err)

	time.Sleep(waitTime)
	testSuit(t, proxy)
}

func TestClash_VlessWS(t *testing.T) {
	cfg := &container.Config{
		Image:        ImageVmess,
		ExposedPorts: defaultExposedPorts,
	}
	hostCfg := &container.HostConfig{
		PortBindings: defaultPortBindings,
		Binds: []string{
			fmt.Sprintf("%s:/etc/v2ray/config.json", C.Path.Resolve("vless-ws.json")),
			fmt.Sprintf("%s:/etc/ssl/v2ray/fullchain.pem", C.Path.Resolve("example.org.pem")),
			fmt.Sprintf("%s:/etc/ssl/v2ray/privkey.pem", C.Path.Resolve("example.org-key.pem")),
		},
	}

	id, err := startContainer(cfg, hostCfg, "vless-ws")
	require.NoError(t, err)
	t.Cleanup(func() {
		cleanContainer(id)
	})

	proxy, err := outbound.NewVless(outbound.VlessOption{
		Name:           "vless",
		Server:         localIP.String(),
		Port:           10002,
		UUID:           "b831381d-6324-4d53-ad4f-8cda48b30811",
		SkipCertVerify: true,
		ServerName:     "example.org",
		Network:        "ws",
		UDP:            true,
	})
	require.NoError(t, err)

	time.Sleep(waitTime)
	testSuit(t, proxy)
}
