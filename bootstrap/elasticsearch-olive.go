package bootstrap

import (
	"context"
	"log"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	coreErrors "github.com/hexlinevault/core-api.git/errors"

	"github.com/olivere/elastic/v7"
)

var (
	elasticMu     sync.RWMutex
	elasticClient *elastic.Client
)

// Elastic elasticsearch management
type Elastic struct {
}

// CreateElasticConnection init elasticsearch connection
func CreateElasticConnection() {
	addrs := strings.Split(os.Getenv("ELASTICSEARCH_HOST"), ";")
	sniff := false
	healthcheck := true
	if v := os.Getenv("ES_ENABLE_SNIFF"); v != "" {
		if vs, err := strconv.ParseBool(v); err == nil {
			sniff = vs
		}
	}
	if v := os.Getenv("ES_ENABLE_HEALTHCHECK"); v != "" {
		if vs, err := strconv.ParseBool(v); err == nil {
			healthcheck = vs
		}
	}
	options := []elastic.ClientOptionFunc{
		elastic.SetURL(addrs...),
		elastic.SetSniff(sniff),
		elastic.SetHealthcheck(healthcheck),
		elastic.SetRetrier(newRetrier()),
		elastic.SetHealthcheckTimeout(15 * time.Second),
		elastic.SetSnifferTimeout(15 * time.Second),
		elastic.SetErrorLog(log.New(os.Stderr, "ELASTIC ", log.LstdFlags)),
		elastic.SetBasicAuth(os.Getenv("ELASTICSEARCH_USERNAME"), os.Getenv("ELASTICSEARCH_PASSWORD")),
	}
	if os.Getenv("ES_DEBUG") == "true" {
		options = append(options, elastic.SetInfoLog(log.New(os.Stdout, "", log.LstdFlags)))
		options = append(options, elastic.SetTraceLog(log.New(os.Stdout, "", log.LstdFlags)))
	}
	client, err := elastic.NewClient(
		options...,
	)
	if err != nil {
		Logger(context.Background()).WithError(err).WithField("component", "elasticsearch").Fatal("Error creating the client")
		return
	}
	elasticMu.Lock()
	elasticClient = client
	elasticMu.Unlock()
	Logger(context.Background()).WithField("component", "elasticsearch").Info("ElasticSearch connected")
}

func CreateElasticConnectionWithHost(host string, username string, password string) *elastic.Client {
	addrs := []string{host}
	// if host == "" {
	// 	addrs = strings.Split(os.Getenv("ELASTICSEARCH_HOST"), ";")
	// }
	sniff := false
	healthcheck := true
	if v := os.Getenv("ES_ENABLE_SNIFF"); v != "" {
		if vs, err := strconv.ParseBool(v); err == nil {
			sniff = vs
		}
	}
	if v := os.Getenv("ES_ENABLE_HEALTHCHECK"); v != "" {
		if vs, err := strconv.ParseBool(v); err == nil {
			healthcheck = vs
		}
	}
	esUname := username
	esPWD := password

	options := []elastic.ClientOptionFunc{
		elastic.SetURL(addrs...),
		elastic.SetSniff(sniff),
		elastic.SetHealthcheck(healthcheck),
		elastic.SetRetrier(newRetrier()),
		elastic.SetHealthcheckTimeout(15 * time.Second),
		elastic.SetSnifferTimeout(15 * time.Second),
		elastic.SetErrorLog(log.New(os.Stderr, "ELASTIC ", log.LstdFlags)),
		elastic.SetBasicAuth(esUname, esPWD),
	}
	if os.Getenv("ES_DEBUG") == "true" {
		options = append(options, elastic.SetInfoLog(log.New(os.Stdout, "", log.LstdFlags)))
		options = append(options, elastic.SetTraceLog(log.New(os.Stdout, "", log.LstdFlags)))
	}
	client, err := elastic.NewClient(
		options...,
	)
	if err != nil {
		Logger(context.Background()).WithError(err).WithField("component", "elasticsearch").Fatal("Error creating the client")
		return nil
	}
	elasticMu.Lock()
	elasticClient = client
	elasticMu.Unlock()
	Logger(context.Background()).WithField("component", "elasticsearch").Info("ElasticSearch connected")
	return elasticClient
}

// Client get Elasticsearch client
func (ctl *Elastic) Client() *elastic.Client {
	elasticMu.RLock()
	defer elasticMu.RUnlock()
	return elasticClient
}

func (ctl *Elastic) HealthCheck() string {
	elasticMu.RLock()
	c := elasticClient
	elasticMu.RUnlock()
	_, err := c.IndexNames()
	if err != nil {
		return err.Error()
	}
	return "ok"
}

type retrier struct {
	backoff elastic.Backoff
}

func newRetrier() *retrier {
	return &retrier{
		backoff: elastic.NewExponentialBackoff(10*time.Millisecond, 8*time.Second),
	}
}

func (r *retrier) Retry(ctx context.Context, retry int, req *http.Request, resp *http.Response, err error) (time.Duration, bool, error) {
	// Fail hard on a specific error
	if err == syscall.ECONNREFUSED {
		return 0, false, coreErrors.ErrElasticsearchNetworkDown
	}

	// Stop after 5 retries
	if retry >= 5 {
		return 0, false, nil
	}

	// Let the backoff strategy decide how long to wait and whether to stop
	wait, stop := r.backoff.Next(retry)
	return wait, stop, nil
}

func (ctl *Elastic) HealthCheckWithResponse() (string, time.Duration) {
	start := time.Now()
	elasticMu.RLock()
	c := elasticClient
	elasticMu.RUnlock()
	_, err := c.IndexNames()
	elapsed := duration("elastic", start)
	if err != nil {
		return err.Error(), elapsed
	}
	return "ok", elapsed
}
