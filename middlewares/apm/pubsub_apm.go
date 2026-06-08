package apmmiddlewares

import (
	"fmt"

	"github.com/hexlinevault/core-api/bootstrap"

	"go.elastic.co/apm"
	"go.elastic.co/apm/module/apmhttp"
)

// APMPubSubWrapper wraps a typed PubSub handler with Elastic APM tracking, the
// PubSub counterpart of APMKafkaWrapper. Apply it at Subscribe time on the api
// side so core's PubSub stays tracer-agnostic:
//
//	bootstrap.PubSub.Subscribe(topic, apmmiddlewares.APMPubSubWrapper(handler))
//
// It starts a "PubSub Consume <topic>" transaction, continues the producer's
// distributed trace from msg.Traceparent (W3C) when present, and exposes the
// transaction to the handler via msg.Context.
func APMPubSubWrapper[T any](fn func(*bootstrap.PubSubMessage, *T) error) func(*bootstrap.PubSubMessage, *T) error {
	return func(msg *bootstrap.PubSubMessage, data *T) error {
		tracer := apm.DefaultTracer
		if !tracer.Recording() {
			return fn(msg, data)
		}

		var opts apm.TransactionOptions
		if msg.Traceparent != "" {
			if tc, err := apmhttp.ParseTraceparentHeader(msg.Traceparent); err == nil {
				opts.TraceContext = tc
			}
		}

		tx := tracer.StartTransactionOptions(fmt.Sprintf("PubSub Consume %s", msg.Topic), "messaging", opts)
		defer tx.End()
		tx.Context.SetLabel("topic", msg.Topic)
		if msg.ID != "" {
			tx.Context.SetLabel("task_id", msg.ID)
		}

		// Make the transaction available to the handler's downstream spans.
		msg.Context = apm.ContextWithTransaction(msg.Context, tx)

		defer func() {
			if r := recover(); r != nil {
				e := tracer.Recovered(r)
				e.SetTransaction(tx)
				e.Send()
				panic(r)
			}
		}()

		err := fn(msg, data)
		if err != nil {
			tx.Result = "failure"
			e := tracer.NewError(err)
			e.SetTransaction(tx)
			e.Send()
			return err
		}
		tx.Result = "success"
		return nil
	}
}
