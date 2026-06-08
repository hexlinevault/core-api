package configs

type SystemNotiConf struct {
	MessageTopic       *string
	TelegramConnection *string
	TelegramChatId     *string // required when use subscriber
	Engine             *string // pubsub engine: "kafka" (default) or "redis"
}
