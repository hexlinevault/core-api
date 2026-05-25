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
}
