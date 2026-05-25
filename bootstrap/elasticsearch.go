package bootstrap

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"github.com/elastic/go-elasticsearch/v9"
	"github.com/elastic/go-elasticsearch/v9/esapi"
	"go.elastic.co/apm/module/apmelasticsearch"
)

var (
	esMu     sync.RWMutex
	esClient *elasticsearch.Client
)

// Elasticsearch elasticsearch management
type Elasticsearch struct {
}

const LanguageMapping = `
    "en" : {
      "type" : "text",
      "fields" : {
        "keyword" : {
          "type" : "keyword",
          "ignore_above" : 256
        }
      }
    },
    "th" : {
      "type" : "text",
      "fields" : {
        "keyword" : {
          "type" : "keyword",
          "ignore_above" : 256
        }
      }
    }`

func MappingProperties(fields ...string) string {
	return `{
		"properties" : {` + strings.Join(fields, `,
		`) + `
		}
	}`
}

func InitElasticIndex(idx string, bodyMapping string) error {
	client := new(Elastic)
	exist, _ := client.Client().IndexExists(idx).Do(context.Background())
	if !exist {
		createIndex, err := client.Client().
			CreateIndex(idx).
			BodyString(`{
			  "mappings" : ` + bodyMapping + `
			}`).
			Do(context.Background())
		if err != nil {
			return err
		}
		if !createIndex.Acknowledged {
			return fmt.Errorf("can not create index %s", idx)
		}
		Logger(context.Background()).WithField("index", idx).Info("Elastic index created")
	}
	return nil
}

// CreateElasticsearchConnection init elasticsearch connection
func CreateElasticsearchConnection() {
	addrs := strings.Split(os.Getenv("ELASTICSEARCH_HOST"), ";")
	cfg := elasticsearch.Config{
		Addresses: addrs,
		Username:  os.Getenv("ELASTICSEARCH_USERNAME"),
		Password:  os.Getenv("ELASTICSEARCH_PASSWORD"),
		Transport: apmelasticsearch.WrapRoundTripper(http.DefaultTransport),
	}
	es, err := elasticsearch.NewClient(cfg)
	if err != nil {
		Logger(context.Background()).WithError(err).WithField("component", "elasticsearch").Fatal("Error creating the client")
		return
	}
	esMu.Lock()
	esClient = es
	esMu.Unlock()

	res, err := es.Info()
	if err != nil {
		Logger(context.Background()).WithError(err).WithField("component", "elasticsearch").Fatal("Error getting response")
		return
	}
	defer res.Body.Close()
	Logger(context.Background()).WithField("component", "elasticsearch").Info("ElasticSearch connected")
}

// Client get Elasticsearch client
func (ctl *Elasticsearch) Client() *elasticsearch.Client {
	esMu.RLock()
	defer esMu.RUnlock()
	return esClient
}

func (ctl *Elasticsearch) HealthCheck() string {
	esMu.RLock()
	c := esClient
	esMu.RUnlock()
	_, err := c.Ping()
	if err != nil {
		return err.Error()
	}
	return "ok"
}

// SearchBuilderData executes a search and returns the raw response.
func (ctl *Elasticsearch) SearchBuilderData(ctx context.Context, read *strings.Reader, index string) (*esapi.Response, error) {
	return ctl.search(ctx, read, index)
}

// SearchBuiderData is an alias kept for backwards compatibility.
//
// Deprecated: use SearchBuilderData.
func (ctl *Elasticsearch) SearchBuiderData(ctx context.Context, read *strings.Reader, index string) (*esapi.Response, error) {
	return ctl.SearchBuilderData(ctx, read, index)
}

// SearchData executes a search and decodes the response into response.
func (ctl *Elasticsearch) SearchData(ctx context.Context, read *strings.Reader, index string, response interface{}) (*esapi.Response, error) {
	res, err := ctl.search(ctx, read, index)
	if err != nil {
		return nil, err
	}
	if err := json.NewDecoder(res.Body).Decode(response); err != nil {
		return res, err
	}
	return res, nil
}

func (ctl *Elasticsearch) search(ctx context.Context, read *strings.Reader, index string) (*esapi.Response, error) {
	esMu.RLock()
	c := esClient
	esMu.RUnlock()
	res, err := c.Search(
		c.Search.WithContext(ctx),
		c.Search.WithIndex(index),
		c.Search.WithBody(read),
		c.Search.WithTrackTotalHits(true),
		c.Search.WithPretty(),
	)
	if err != nil {
		return nil, err
	}
	return res, nil
}

// DecodeResponse decode response model or map
func (ctl *Elasticsearch) DecodeResponse(body io.ReadCloser, response interface{}) error {
	if err := json.NewDecoder(body).Decode(response); err != nil {
		return err
	}
	return nil
}
