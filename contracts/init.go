package contracts

import (
	"github.com/hexlinevault/core-api/bootstrap"
)

type Contract struct {
	redis bootstrap.Redis
}

var contract = new(Contract)
