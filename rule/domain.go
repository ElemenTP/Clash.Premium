package rules

import (
	"strings"

	C "github.com/Dreamacro/clash/constant"
)

type Domain struct {
	*Base
	domain  string
	adapter string
}

func (d *Domain) RuleType() C.RuleType {
	return C.Domain
}

func (d *Domain) Match(metadata *C.Metadata) bool {
	return metadata.Host == d.domain
}

func (d *Domain) Adapter() string {
	return d.adapter
}

func (d *Domain) Payload() string {
	return d.domain
}

func (d *Domain) ShouldResolveIP() bool {
	return false
}

func NewDomain(domain string, adapter string) *Domain {
	return &Domain{
		Base:    &Base{},
		domain:  strings.ToLower(domain),
		adapter: adapter,
	}
}

var _ C.Rule = (*Domain)(nil)
