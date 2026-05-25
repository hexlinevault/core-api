package bootstrap

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"
	"os"
	"runtime"
	"strings"
	"time"

	logrustash "github.com/bshuster-repo/logrus-logstash-hook"
	"github.com/gin-gonic/gin"
	"github.com/sirupsen/logrus"
	"go.elastic.co/apm"
	"go.elastic.co/apm/module/apmlogrus"
)

// Context key for correlation ID (used when APM is not available)
type correlationIDKey struct{}

// ContextWithCorrelationID adds a correlation ID to the context
func ContextWithCorrelationID(ctx context.Context, correlationID string) context.Context {
	return context.WithValue(ctx, correlationIDKey{}, correlationID)
}

// CorrelationIDFromContext extracts correlation ID from context
func CorrelationIDFromContext(ctx context.Context) string {
	if id, ok := ctx.Value(correlationIDKey{}).(string); ok {
		return id
	}
	return ""
}

// GenerateCorrelationID generates a random correlation ID (32 hex chars like APM trace ID)
func GenerateCorrelationID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

// callerPrettyfier formats caller info for JSON output
func callerPrettyfier(f *runtime.Frame) (string, string) {
	// Return function name and file:line
	return f.Function, fmt.Sprintf("%s:%d", f.File, f.Line)
}

var logstashLogger *logrus.Logger

func CreateLogstashConnection(serviceName string) {
	logstashLogger = logrus.New()

	// Enable caller reporting - this will show the actual caller, not the Logger() helper
	logstashLogger.SetReportCaller(true)

	// Set formatter from environment variable
	formatterType := os.Getenv("LOG_FORMATTER")
	switch strings.ToLower(formatterType) {
	case "text":
		logstashLogger.Formatter = &logrus.TextFormatter{}
	case "json", "":
		// Default to JSON if not specified or empty
		logstashLogger.Formatter = &logrus.JSONFormatter{
			CallerPrettyfier: callerPrettyfier,
		}
	default:
		// Default to JSON for unknown formatter types
		logstashLogger.Formatter = &logrus.JSONFormatter{
			CallerPrettyfier: callerPrettyfier,
		}
	}

	logstashLogger.AddHook(&apmlogrus.Hook{})
	if v := os.Getenv("LOGSTASH_HOST"); v != "" {
		protocol := "udp"
		if v := os.Getenv("LOGSTASH_PROTOCOL"); v != "" {
			if v != "tcp" && v != "udp" {
				panic("[logstash] invalid protocol (protocol must be only udp or tcp)")
			}
			protocol = v
		}
		conn, err := net.Dial(protocol, v)
		if err != nil {
			panic("[logstash] error : " + err.Error())
		}
		hostname, _ := os.Hostname()
		addrs, _ := net.LookupHost(hostname)
		hook := logrustash.New(conn, logrustash.DefaultFormatter(logrus.Fields{
			"service_name": serviceName,
			"facility":     os.Getenv("APP_ENV"),
			"app_debug":    os.Getenv("APP_DEBUG"),
			"app_version":  os.Getenv("APP_VERSION"),
			"host_name":    hostname,
			"host_addrs":   addrs,
		}))

		logstashLogger.Hooks.Add(hook)
		fmt.Println("[logstash] connected")
	}
}

// Logger using logger with context
func Logger(v context.Context) *logrus.Entry {
	ct := context.TODO()
	clientIP := ""
	var userID string

	switch c := v.(type) {
	case *gin.Context:
		clientIP = c.GetHeader("X-Forwarded-For")
		ct = c.Request.Context()
		// Auto-extract user_id from gin context
		userID = c.GetString("user_id")
	case context.Context:
		ct = c
	}

	// Get correlation ID - priority: 1) Custom context value, 2) APM trace ID
	correlationID := CorrelationIDFromContext(ct)

	tx := apm.TransactionFromContext(ct)
	if correlationID == "" && tx != nil {
		// Use APM trace ID if no custom correlation ID
		correlationID = tx.TraceContext().Trace.String()
	}
	// Note: Don't create new transaction if none exists in context
	// This prevents different correlation_ids for logs within the same request/job
	// The calling code should create the transaction and pass it via context

	internalIP := ""
	if strings.Contains(clientIP, ",") {
		ips := strings.Split(clientIP, ",")
		if len(ips) > 1 {
			clientIP = ips[0]
			internalIP = ips[1]
		}
	}

	fields := logrus.Fields{
		"correlation_id": correlationID,
		"client_ip":      clientIP,
		"extends_ip":     internalIP,
	}

	// Add APM fields safely
	if tx != nil {
		if tx.Name != "" {
			fields["endpoint"] = tx.Name
		}
		if tx.Type != "" {
			fields["log_type"] = tx.Type
		}
		fields["execute_time"] = tx.Duration
	}

	// Add user_id if available
	if userID != "" {
		fields["user_id"] = userID
	}

	// Note: Caller info is now handled by logrus ReportCaller feature
	// which captures the actual call site of .Info()/.Warn()/.Error() etc.

	// Handle nil logstashLogger gracefully
	if logstashLogger == nil {
		// Fallback to standard logger if logstash not initialized
		CreateLogstashConnection("default")
	}

	return logstashLogger.WithContext(ct).WithFields(fields)
}

func HealthCheck() string {
	_, err := net.Dial("tcp", os.Getenv("LOGSTASH_HOST"))
	if err != nil {
		return err.Error()
	} else {
		return "ok"
	}
}

func HealthCheckWithResponse() (string, time.Duration) {
	start := time.Now()
	_, err := net.Dial("tcp", os.Getenv("LOGSTASH_HOST"))
	time := duration("logstash", start)
	responseTime := time
	if err != nil {
		return err.Error(), responseTime
	} else {
		return "ok", responseTime
	}
}
