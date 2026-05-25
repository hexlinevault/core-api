package apmmiddlewares

import (
	"github.com/gin-gonic/gin"
	"github.com/hexlinevault/core-api.git/bootstrap"
	"go.elastic.co/apm"
)

// CorrelationMiddleware creates a middleware that ensures every request has a correlation ID
// If APM is available, it uses APM transaction trace ID
// Otherwise, it generates a custom correlation ID
func CorrelationMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx := c.Request.Context()

		// Check if APM transaction exists (from apmgin middleware)
		tx := apm.TransactionFromContext(ctx)

		if tx == nil {
			// No APM transaction - create custom correlation ID
			correlationID := bootstrap.GenerateCorrelationID()
			ctx = bootstrap.ContextWithCorrelationID(ctx, correlationID)
			c.Request = c.Request.WithContext(ctx)
		}
		// If APM transaction exists, correlation ID will be extracted from it in Logger()

		c.Next()
	}
}

// APMGinMiddleware creates APM transaction for each request
// Use this as the first middleware to enable APM tracing and correlation ID
func APMGinMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if apm.DefaultTracer == nil {
			// APM not configured, skip
			c.Next()
			return
		}

		// Create APM transaction for this request
		txName := c.Request.Method + " " + c.FullPath()
		if txName == " " {
			txName = c.Request.Method + " " + c.Request.URL.Path
		}

		tx := apm.DefaultTracer.StartTransaction(txName, "request")
		defer tx.End()

		// Set transaction context
		ctx := apm.ContextWithTransaction(c.Request.Context(), tx)
		c.Request = c.Request.WithContext(ctx)

		c.Next()

		// Set result based on status code
		statusCode := c.Writer.Status()
		tx.Result = statusText(statusCode)
		if statusCode >= 500 {
			tx.Outcome = "failure"
		} else {
			tx.Outcome = "success"
		}
	}
}

func statusText(code int) string {
	switch {
	case code >= 500:
		return "HTTP 5xx"
	case code >= 400:
		return "HTTP 4xx"
	case code >= 300:
		return "HTTP 3xx"
	case code >= 200:
		return "HTTP 2xx"
	default:
		return "HTTP 1xx"
	}
}
