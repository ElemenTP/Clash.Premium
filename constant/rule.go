package constant

import "github.com/Dreamacro/clash/component/geodata/router"

// Rule Type
const (
	Domain RuleType = iota
	DomainSuffix
	DomainKeyword
	GEOSITE
	GEOIP
	IPCIDR
	SrcIPCIDR
	SrcPort
	DstPort
	Process
	ProcessPath
	Script
	UserAgent
	MATCH
)

type RuleType int

func (rt RuleType) String() string {
	switch rt {
	case Domain:
		return "Domain"
	case DomainSuffix:
		return "DomainSuffix"
	case DomainKeyword:
		return "DomainKeyword"
	case GEOSITE:
		return "GeoSite"
	case GEOIP:
		return "GeoIP"
	case IPCIDR:
		return "IPCIDR"
	case SrcIPCIDR:
		return "SrcIPCIDR"
	case SrcPort:
		return "SrcPort"
	case DstPort:
		return "DstPort"
	case Process:
		return "Process"
	case ProcessPath:
		return "ProcessPath"
	case Script:
		return "Script"
	case UserAgent:
		return "UserAgent"
	case MATCH:
		return "Match"
	default:
		return "Unknown"
	}
}

type Rule interface {
	RuleType() RuleType
	Match(metadata *Metadata) bool
	Adapter() string
	Payload() string
	ShouldResolveIP() bool
	ShouldFindProcess() bool
}

type RuleGeoSite interface {
	GetDomainMatcher() *router.DomainMatcher
}
