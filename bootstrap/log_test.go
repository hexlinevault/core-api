package bootstrap_test

import (
	"context"
	"testing"

	"github.com/hexlinevault/core-api.git/bootstrap"
)

func TestPrintLog(t *testing.T) {
	bootstrap.CreateLogstashConnection("test-service")
	bootstrap.Logger(context.TODO()).Error("test")
}
