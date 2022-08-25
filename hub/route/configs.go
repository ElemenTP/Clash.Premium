package route

import (
	"encoding/json"
	"net/http"
	"path/filepath"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/render"

	"github.com/Dreamacro/clash/component/dialer"
	"github.com/Dreamacro/clash/component/resolver"
	"github.com/Dreamacro/clash/config"
	"github.com/Dreamacro/clash/constant"
	"github.com/Dreamacro/clash/hub/executor"
	P "github.com/Dreamacro/clash/listener"
	"github.com/Dreamacro/clash/listener/tun/ipstack/commons"
	"github.com/Dreamacro/clash/log"
	"github.com/Dreamacro/clash/tunnel"
)

func configRouter() http.Handler {
	r := chi.NewRouter()
	r.Get("/", getConfigs)
	r.Put("/", updateConfigs)
	r.Patch("/", patchConfigs)
	return r
}

type configSchema struct {
	Port        *int               `json:"port,omitempty"`
	SocksPort   *int               `json:"socks-port,omitempty"`
	RedirPort   *int               `json:"redir-port,omitempty"`
	TProxyPort  *int               `json:"tproxy-port,omitempty"`
	MixedPort   *int               `json:"mixed-port,omitempty"`
	MitmPort    *int               `json:"mitm-port,omitempty"`
	AllowLan    *bool              `json:"allow-lan,omitempty"`
	BindAddress *string            `json:"bind-address,omitempty"`
	Mode        *tunnel.TunnelMode `json:"mode,omitempty"`
	LogLevel    *log.LogLevel      `json:"log-level,omitempty"`
	IPv6        *bool              `json:"ipv6,omitempty"`
	Sniffing    *bool              `json:"sniffing,omitempty"`
	Tun         *tunConfigSchema   `json:"tun,omitempty"`
}

type tunConfigSchema struct {
	Enable              *bool              `json:"enable,omitempty"`
	Device              *string            `json:"device,omitempty"`
	Stack               *constant.TUNStack `json:"stack,omitempty"`
	DNSHijack           *[]constant.DNSUrl `json:"dns-hijack,omitempty"`
	AutoRoute           *bool              `json:"auto-route,omitempty"`
	AutoDetectInterface *bool              `json:"auto-detect-interface,omitempty"`
}

func getConfigs(w http.ResponseWriter, r *http.Request) {
	general := executor.GetGeneral()
	render.JSON(w, r, general)
}

func pointerOrDefault(p *int, def int) int {
	if p != nil {
		return *p
	}

	return def
}

func patchConfigs(w http.ResponseWriter, r *http.Request) {
	general := &configSchema{}
	if err := render.DecodeJSON(r.Body, general); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrBadRequest)
		return
	}

	if general.AllowLan != nil {
		P.SetAllowLan(*general.AllowLan)
	}

	if general.BindAddress != nil {
		P.SetBindAddress(*general.BindAddress)
	}

	ports := P.GetPorts()

	tcpIn := tunnel.TCPIn()
	udpIn := tunnel.UDPIn()

	P.ReCreateHTTP(pointerOrDefault(general.Port, ports.Port), tcpIn)
	P.ReCreateSocks(pointerOrDefault(general.SocksPort, ports.SocksPort), tcpIn, udpIn)
	P.ReCreateRedir(pointerOrDefault(general.RedirPort, ports.RedirPort), tcpIn, udpIn)
	P.ReCreateTProxy(pointerOrDefault(general.TProxyPort, ports.TProxyPort), tcpIn, udpIn)
	P.ReCreateMixed(pointerOrDefault(general.MixedPort, ports.MixedPort), tcpIn, udpIn)
	P.ReCreateMitm(pointerOrDefault(general.MitmPort, ports.MitmPort), tcpIn)

	if general.Mode != nil {
		tunnel.SetMode(*general.Mode)
	}

	if general.LogLevel != nil {
		log.SetLevel(*general.LogLevel)
	}

	if general.IPv6 != nil {
		resolver.DisableIPv6 = !*general.IPv6
	}

	if general.Sniffing != nil {
		tunnel.SetSniffing(*general.Sniffing)
	}

	if general.Tun != nil {
		tunSchema := general.Tun
		tunConf := P.GetTunConf()

		if tunSchema.Enable != nil {
			tunConf.Enable = *tunSchema.Enable
		}
		if tunSchema.Device != nil {
			tunConf.Device = *tunSchema.Device
		}
		if tunSchema.Stack != nil {
			tunConf.Stack = *tunSchema.Stack
		}
		if tunSchema.DNSHijack != nil {
			tunConf.DNSHijack = *tunSchema.DNSHijack
		}
		if tunSchema.AutoRoute != nil {
			tunConf.AutoRoute = *tunSchema.AutoRoute
		}
		if tunSchema.AutoDetectInterface != nil {
			tunConf.AutoDetectInterface = *tunSchema.AutoDetectInterface
		}

		if dialer.DefaultInterface.Load() == "" && tunConf.Enable {
			outboundInterface, _ := commons.GetAutoDetectInterface()
			if outboundInterface != "" {
				dialer.DefaultInterface.Store(outboundInterface)
			}
		}

		P.ReCreateTun(&tunConf, tcpIn, udpIn)
		P.ReCreateRedirToTun(tunConf.RedirectToTun)
	}

	msg, _ := json.Marshal(general)
	log.Warnln("[RESTful API] patch config by: %s", string(msg))

	render.NoContent(w, r)
}

type updateConfigRequest struct {
	Path    string `json:"path"`
	Payload string `json:"payload"`
}

func updateConfigs(w http.ResponseWriter, r *http.Request) {
	req := updateConfigRequest{}
	if err := render.DecodeJSON(r.Body, &req); err != nil {
		render.Status(r, http.StatusBadRequest)
		render.JSON(w, r, ErrBadRequest)
		return
	}

	force := r.URL.Query().Get("force") == "true"
	var cfg *config.Config
	var err error

	if req.Payload != "" {
		log.Warnln("[RESTful API] update config by payload")
		cfg, err = executor.ParseWithBytes([]byte(req.Payload))
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, newError(err.Error()))
			return
		}
	} else {
		if req.Path == "" {
			req.Path = constant.Path.Config()
		}
		if !filepath.IsAbs(req.Path) {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, newError("path is not a absolute path"))
			return
		}

		log.Warnln("[RESTful API] reload config from path: %s", req.Path)
		cfg, err = executor.ParseWithPath(req.Path)
		if err != nil {
			render.Status(r, http.StatusBadRequest)
			render.JSON(w, r, newError(err.Error()))
			return
		}
	}

	executor.ApplyConfig(cfg, force)
	render.NoContent(w, r)
}
