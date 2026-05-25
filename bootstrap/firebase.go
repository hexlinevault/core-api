package bootstrap

import (
	"context"
	"sync"

	firebase "firebase.google.com/go"
	"firebase.google.com/go/messaging"
	"google.golang.org/api/option"
)

var (
	firebaseMu sync.RWMutex
	fApp       map[string]*firebase.App    = make(map[string]*firebase.App)
	fMessaging map[string]*messaging.Client = make(map[string]*messaging.Client)
)

type Firebase struct {
}

// CreateFirebaseConnection init firebase connection
// example
// bootstrap.CreateFirebaseConnection("storage/service-account")
// use case example new(Firebase).Messaging()
// bootstrap.CreateFirebaseConnection("storage/service-account", "staging")
// use case example new(Firebase).Messaging("staging")
func CreateFirebaseConnection(serviceAccountPath string, connectionNames ...string) {
	connectionName := resolveConnectionName(connectionNames)
	opt := option.WithCredentialsFile(serviceAccountPath)
	var err error
	app, err := firebase.NewApp(context.Background(), nil, opt)
	if err != nil {
		Logger(context.Background()).WithError(err).WithField("connection_name", connectionName).WithField("component", "firebase").Fatal("error initializing app")
		return
	}
	msg, err := app.Messaging(context.Background())
	if err != nil {
		Logger(context.Background()).WithError(err).WithField("connection_name", connectionName).WithField("component", "firebase").Fatal("error initializing messaging")
		return
	}
	firebaseMu.Lock()
	fApp[connectionName] = app
	fMessaging[connectionName] = msg
	firebaseMu.Unlock()
}

// Firebase get filebase connection
func (ctl *Firebase) Firebase(connectionNames ...string) *firebase.App {
	connectionName := resolveConnectionName(connectionNames)
	firebaseMu.RLock()
	defer firebaseMu.RUnlock()
	return fApp[connectionName]
}

// Messaging get messaging function (FCM)
func (ctl *Firebase) Messaging(connectionNames ...string) *messaging.Client {
	connectionName := resolveConnectionName(connectionNames)
	firebaseMu.RLock()
	defer firebaseMu.RUnlock()
	return fMessaging[connectionName]
}
