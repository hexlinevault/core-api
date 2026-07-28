package configs

import (
	"github.com/IBM/sarama"
)

type KafkaConn struct {
	Brokers        []string
	ConsumerGroup  string
	Version        string
	Assignor       string
	Oldest         bool
	Verbose        bool
	ProducerRetry  int
	ConsumerConfig *sarama.Config
	ProducerConfig *sarama.Config
	Prefix         string
	TLS            bool

	// Producer throughput tuning. All optional — a zero value keeps the previous
	// behavior (sarama defaults), so existing callers are unaffected. Raising
	// batching + in-flight is the lever for high-rate producers whose bottleneck
	// is the default MaxOpenRequests=5 with no batching.
	ProducerFlushFrequencyMs int    // linger: batch pending messages up to this long before a produce request (0 = off)
	ProducerFlushMessages    int    // also flush once this many messages are pending (0 = off)
	ProducerMaxOpenRequests  int    // max in-flight produce requests per broker (0 = sarama default 5)
	ProducerCompression      string // "gzip" | "snappy" | "lz4" | "zstd" ("" = none)
}
