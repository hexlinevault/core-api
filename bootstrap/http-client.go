package bootstrap

import (
	"go.elastic.co/apm/module/apmhttp"
	"net/http"
)

var tracingClient *http.Client

func InitHttpClient(c *http.Client) {
	if c == nil {
		tracingClient = apmhttp.WrapClient(http.DefaultClient)
	} else {
		tracingClient = apmhttp.WrapClient(c)
	}
}

func GetHTTPClient() *http.Client {
	if tracingClient == nil {
		return apmhttp.WrapClient(http.DefaultClient)
	}
	return tracingClient
}
