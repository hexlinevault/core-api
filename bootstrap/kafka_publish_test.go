package bootstrap

import (
	"testing"

	"github.com/IBM/sarama"
	"github.com/stretchr/testify/assert"
)

func TestKafkaPublishOptions(t *testing.T) {
	opts := &publishOptions{Topic: "t"}
	for _, o := range []PublishOption{
		WithKey("fixture-42"),
		WithKafkaHeader("lsports-type", "3"),
		WithKafkaHeader("lsports-seq", "77"),
	} {
		o(opts)
	}

	assert.Equal(t, "fixture-42", opts.Key)
	assert.Equal(t, []sarama.RecordHeader{
		{Key: []byte("lsports-type"), Value: []byte("3")},
		{Key: []byte("lsports-seq"), Value: []byte("77")},
	}, opts.Headers)
}
