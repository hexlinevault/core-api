package configs

type SystemNotiConf struct {
	MessageTopic       *string
	TelegramConnection *string
	TelegramChatId     *string // required when use subscriber
}
