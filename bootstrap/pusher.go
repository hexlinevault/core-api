package bootstrap

import (
	"github.com/pusher/pusher-http-go"
	"os"
)

var pusherClient *pusher.Client

type Pusher struct {

}

func InitPusherClient()  {
	pusherClient = &pusher.Client{
		AppID:   os.Getenv("PUSHER_APP_ID"),
		Key:     os.Getenv("PUSHER_APP_KEY"),
		Secret:  os.Getenv("PUSHER_APP_SECRET"),
		Cluster: os.Getenv("PUSHER_APP_CLUSTER"),
		Secure:  true,
	}
}

func (p *Pusher) GetClient() *pusher.Client {
	return pusherClient
}
