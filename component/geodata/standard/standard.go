package standard

import (
	"fmt"
	"io"
	"os"
	"strings"

	"google.golang.org/protobuf/proto"

	"github.com/Dreamacro/clash/component/geodata"
	"github.com/Dreamacro/clash/component/geodata/router"
	C "github.com/Dreamacro/clash/constant"
)

func ReadFile(path string) ([]byte, error) {
	reader, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func(reader *os.File) {
		_ = reader.Close()
	}(reader)

	return io.ReadAll(reader)
}

func ReadAsset(file string) ([]byte, error) {
	return ReadFile(C.Path.Resolve(file))
}

func loadIP(filename, country string) ([]*router.CIDR, error) {
	geoipBytes, err := ReadAsset(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %s, base error: %s", filename, err.Error())
	}
	var geoipList router.GeoIPList
	if err := proto.Unmarshal(geoipBytes, &geoipList); err != nil {
		return nil, err
	}

	for _, geoip := range geoipList.Entry {
		if strings.EqualFold(geoip.CountryCode, country) {
			return geoip.Cidr, nil
		}
	}

	return nil, fmt.Errorf("country not found in %s%s%s", filename, ": ", country)
}

func loadSite(filename, list string) ([]*router.Domain, error) {
	geositeBytes, err := ReadAsset(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to open file: %s, base error: %s", filename, err.Error())
	}
	var geositeList router.GeoSiteList
	if err := proto.Unmarshal(geositeBytes, &geositeList); err != nil {
		return nil, err
	}

	for _, site := range geositeList.Entry {
		if strings.EqualFold(site.CountryCode, list) {
			return site.Domain, nil
		}
	}

	return nil, fmt.Errorf("list not found in %s%s%s", filename, ": ", list)
}

type standardLoader struct{}

func (d standardLoader) LoadSite(filename, list string) ([]*router.Domain, error) {
	return loadSite(filename, list)
}

func (d standardLoader) LoadIP(filename, country string) ([]*router.CIDR, error) {
	return loadIP(filename, country)
}

func init() {
	geodata.RegisterGeoDataLoaderImplementationCreator("standard", func() geodata.LoaderImplementation {
		return standardLoader{}
	})
}
