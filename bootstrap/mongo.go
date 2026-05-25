package bootstrap

import (
	"context"
	"sync"

	"github.com/hexlinevault/core-api.git/configs"

	"go.mongodb.org/mongo-driver/mongo"
)

type (
	// MongoDB database management
	Mongo struct {
	}
)

// dbMongo variable for define connection
var (
	mongoMu sync.RWMutex
	dbMongo map[string]*mongo.Client = make(map[string]*mongo.Client)
)

// CreateMongoConnection make connection
func CreateMongoConnection(conf *configs.MongoConn) *mongo.Client {
	connectionName := resolveConnectionName([]string{conf.ConnectionName})

	client, err := mongo.Connect(context.TODO(), conf.Options...)
	if err != nil {
		Logger(context.Background()).WithError(err).WithField("connection_name", connectionName).WithField("component", "mongodb").Fatal("Failed to connect database")
	}
	Logger(context.Background()).WithField("connection_name", connectionName).WithField("component", "mongodb").Info("Database connected")
	mongoMu.Lock()
	dbMongo[connectionName] = client
	mongoMu.Unlock()
	return client
}

// Client get mongo connection client
func (c *Mongo) Client(connectionNames ...string) *mongo.Client {
	connectionName := resolveConnectionName(connectionNames)
	mongoMu.RLock()
	defer mongoMu.RUnlock()
	return dbMongo[connectionName]
}
