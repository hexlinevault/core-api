package bootstrap

import (
	"fmt"

	"github.com/oschwald/geoip2-golang"
)

type GeoIP struct{}

var geoIPDB map[string]*geoip2.Reader = make(map[string]*geoip2.Reader)

func (h *GeoIP) DB(dbTypes ...string) *geoip2.Reader {
	dbType := "default"
	if len(dbTypes) > 0 {
		dbType = dbTypes[0]
	}
	return geoIPDB[dbType]
}

func CreateGeoIP(f string, dbTypes ...string) *geoip2.Reader {
	geoIP, err := geoip2.Open(f)
	dbType := "default"
	if len(dbTypes) > 0 {
		dbType = dbTypes[0]
	}
	if err != nil {
		panic(fmt.Sprintf("[geoip:%s] failed to load geoip database file %s %s", dbType, f, err))
	}
	fmt.Printf("[geoip:%s] loaded\n", dbType)
	geoIPDB[dbType] = geoIP
	return geoIP
}
