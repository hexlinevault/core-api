package configs

import (
	"net/url"
	"time"
)

type RabbitMQConn struct {
	// URL is the full AMQP URI (amqp://user:pass@host:5672/vhost).
	// If set, it takes precedence over Hosts/Username/Password/VHost.
	URL string

	// Hosts is the list of broker nodes (host:port). The service dials them
	// in order and fails over to the next node on connection loss.
	Hosts    []string
	Username string
	Password string
	VHost    string

	// ConnectionName is shown in the RabbitMQ management UI.
	ConnectionName string

	// Prefix is prepended to every exchange and queue name (same idea as KafkaConn.Prefix).
	Prefix string

	// PrefetchCount is the default per-consumer QoS prefetch (default 10).
	PrefetchCount int

	// ReconnectDelay is the base delay between reconnect attempts,
	// backed off exponentially up to 30s (default 2s).
	ReconnectDelay time.Duration

	// PublisherConfirm enables publisher confirms: Publish blocks until the
	// broker acks the message (default false).
	PublisherConfirm bool

	// PublishTimeout bounds a confirmed publish (default 10s).
	PublishTimeout time.Duration

	// Heartbeat is the AMQP heartbeat interval (default 10s).
	Heartbeat time.Duration
}

// URLs returns the list of AMQP URIs to try, built from URL or Hosts.
func (c *RabbitMQConn) URLs() []string {
	if c.URL != "" {
		return []string{c.URL}
	}
	vhost := c.VHost
	if vhost == "/" {
		vhost = "" // "amqp://host/" already means the default vhost
	}
	urls := make([]string, 0, len(c.Hosts))
	for _, host := range c.Hosts {
		u := url.URL{
			Scheme: "amqp",
			User:   url.UserPassword(c.Username, c.Password),
			Host:   host,
			Path:   "/" + url.PathEscape(vhost),
		}
		urls = append(urls, u.String())
	}
	return urls
}
