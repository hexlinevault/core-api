package apmmiddlewares

import (
	"context"
	"fmt"

	"github.com/IBM/sarama"
	"go.elastic.co/apm"
	"go.elastic.co/apm/module/apmhttp"
)

// ConsumerHandlerFunc is the type for the Kafka message handler
type ConsumerHandlerFunc func(context.Context, *sarama.ConsumerMessage) error

// APMKafkaWrapper wraps a sarama consumer handler with Elastic APM tracking
func APMKafkaWrapper[T any](fn func(*sarama.ConsumerMessage, T) error) func(*sarama.ConsumerMessage, T) error {
	return func(msg *sarama.ConsumerMessage, data T) error {
		tracer := apm.DefaultTracer
		if !tracer.Recording() {
			return fn(msg, data)
		}

		// Extract trace context from message headers
		traceContext := getKafkaTraceContext(msg.Headers)
		transactionOpts := apm.TransactionOptions{
			TraceContext: traceContext,
		}

		// Start a transaction
		tx := tracer.StartTransactionOptions(fmt.Sprintf("Kafka Consume %s", msg.Topic), "messaging", transactionOpts)
		defer tx.End()

		// Set context info
		tx.Context.SetLabel("topic", msg.Topic)
		tx.Context.SetLabel("partition", msg.Partition)
		tx.Context.SetLabel("offset", msg.Offset)
		if len(msg.Key) > 0 {
			tx.Context.SetLabel("key", string(msg.Key))
		}

		// Create a context with the transaction
		_ = apm.ContextWithTransaction(context.Background(), tx)

		// Execute handler
		// We catch panic to ensure transaction is ended properly and error is logged
		defer func() {
			if r := recover(); r != nil {
				e := tracer.Recovered(r)
				e.SetTransaction(tx)
				e.Send()
				// Re-panic if needed or handle gracefully
				panic(r)
			}
		}()

		// Call the handler and capture the error
		err := fn(msg, data)

		if err != nil {
			tx.Result = "failure"
			// Capture error in APM
			e := tracer.NewError(err)
			e.SetTransaction(tx)
			e.Send()
			return err
		}

		tx.Result = "success"
		return nil
	}
}

func getKafkaTraceContext(headers []*sarama.RecordHeader) apm.TraceContext {
	// Look for W3C Traceparent header
	for _, h := range headers {
		if string(h.Key) == "elastic-apm-traceparent" || string(h.Key) == "traceparent" {
			if traceContext, err := apmhttp.ParseTraceparentHeader(string(h.Value)); err == nil {
				return traceContext
			}
		}
	}
	return apm.TraceContext{}
}
