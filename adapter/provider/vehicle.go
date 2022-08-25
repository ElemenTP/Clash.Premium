package provider

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"time"

	"github.com/Dreamacro/clash/common/convert"
	"github.com/Dreamacro/clash/component/dialer"
	types "github.com/Dreamacro/clash/constant/provider"
)

type FileVehicle struct {
	path string
}

func (f *FileVehicle) Type() types.VehicleType {
	return types.File
}

func (f *FileVehicle) Path() string {
	return f.path
}

func (f *FileVehicle) Read() ([]byte, error) {
	return os.ReadFile(f.path)
}

func NewFileVehicle(path string) *FileVehicle {
	return &FileVehicle{path: path}
}

type HTTPVehicle struct {
	path     string
	url      string
	urlProxy bool
	header   http.Header
}

func (h *HTTPVehicle) Type() types.VehicleType {
	return types.HTTP
}

func (h *HTTPVehicle) Path() string {
	return h.path
}

func (h *HTTPVehicle) Read() ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*20)
	defer cancel()

	uri, err := url.Parse(h.url)
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequest(http.MethodGet, uri.String(), nil)
	if err != nil {
		return nil, err
	}

	if h.header != nil {
		req.Header = h.header
	}

	if user := uri.User; user != nil {
		password, _ := user.Password()
		req.SetBasicAuth(user.Username(), password)
	}

	convert.SetUserAgent(req.Header)

	req = req.WithContext(ctx)

	transport := &http.Transport{
		// from http.DefaultTransport
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if h.urlProxy {
				// make sure the tun device is turned on, and do not reject the Clash traffic by rule `PROCESS-NAME`
				return (&net.Dialer{}).DialContext(ctx, network, address) // forward to tun if tun enabled
			}
			return dialer.DialContext(ctx, network, address, dialer.WithDirect()) // with direct
		},
	}

	client := http.Client{Transport: transport}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = resp.Body.Close()
	}()

	buf, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	return removeComment(buf), nil
}

func NewHTTPVehicle(path string, url string, urlProxy bool, header http.Header) *HTTPVehicle {
	return &HTTPVehicle{path, url, urlProxy, header}
}
