package configs

import (
	"go.mongodb.org/mongo-driver/mongo/options"
)

type MongoConn struct {
	ConnectionName string // empty is default
	Options        []*options.ClientOptions
}
