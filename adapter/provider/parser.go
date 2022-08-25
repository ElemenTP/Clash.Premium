package provider

import (
	"errors"
	"fmt"
	"time"

	"github.com/Dreamacro/clash/common/structure"
	C "github.com/Dreamacro/clash/constant"
	types "github.com/Dreamacro/clash/constant/provider"
)

var errVehicleType = errors.New("unsupport vehicle type")

type healthCheckSchema struct {
	Enable   bool   `provider:"enable"`
	URL      string `provider:"url"`
	Interval int    `provider:"interval"`
	Lazy     bool   `provider:"lazy,omitempty"`
}

type proxyProviderSchema struct {
	Type            string              `provider:"type"`
	Path            string              `provider:"path"`
	URL             string              `provider:"url,omitempty"`
	URLProxy        bool                `provider:"url-proxy,omitempty"`
	Interval        int                 `provider:"interval,omitempty"`
	Filter          string              `provider:"filter,omitempty"`
	HealthCheck     healthCheckSchema   `provider:"health-check,omitempty"`
	ForceCertVerify bool                `provider:"force-cert-verify,omitempty"`
	PrefixName      string              `provider:"prefix-name,omitempty"`
	Header          map[string][]string `provider:"header,omitempty"`
}

func ParseProxyProvider(name string, mapping map[string]any, forceCertVerify bool) (types.ProxyProvider, error) {
	decoder := structure.NewDecoder(structure.Option{TagName: "provider", WeaklyTypedInput: true})

	schema := &proxyProviderSchema{
		HealthCheck: healthCheckSchema{
			Lazy: true,
		},
	}

	if forceCertVerify {
		schema.ForceCertVerify = true
	}

	if err := decoder.Decode(mapping, schema); err != nil {
		return nil, err
	}

	var hcInterval uint
	if schema.HealthCheck.Enable {
		hcInterval = uint(schema.HealthCheck.Interval)
	}
	hc := NewHealthCheck([]C.Proxy{}, schema.HealthCheck.URL, hcInterval, schema.HealthCheck.Lazy)

	path := C.Path.Resolve(schema.Path)

	var vehicle types.Vehicle
	switch schema.Type {
	case "file":
		vehicle = NewFileVehicle(path)
	case "http":
		vehicle = NewHTTPVehicle(path, schema.URL, schema.URLProxy, schema.Header)
	default:
		return nil, fmt.Errorf("%w: %s", errVehicleType, schema.Type)
	}

	interval := time.Duration(uint(schema.Interval)) * time.Second
	filter := schema.Filter
	return NewProxySetProvider(name, interval, filter, vehicle, hc, schema.ForceCertVerify, schema.PrefixName)
}