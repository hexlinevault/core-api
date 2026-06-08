package bootstrap

import (
	"fmt"

	"github.com/hexlinevault/core-api/configs"
)

// SystemNotiEngine selects the pubsub backend used to deliver system notifications.
type SystemNotiEngine string

const (
	SystemNotiEngineKafka SystemNotiEngine = "kafka"
	SystemNotiEngineRedis SystemNotiEngine = "redis"
)

var SYSTEM_NOTI_KAFKA_PUBLISHER = "system-notification-publisher"
var SYSTEM_NOTI_TELEGRAM_CONNECTION = "default"
var SYSTEM_NOTI_TELEGRAM_BOT_CHAT_ID = ""

// SYSTEM_NOTI_ENGINE chooses which pubsub engine TriggerSystemNoti publishes to
// and which subscriber to register. Defaults to Kafka for backward compatibility.
var SYSTEM_NOTI_ENGINE = SystemNotiEngineKafka

// InitSystemNoti init system notification
func InitSystemNoti(req *configs.SystemNotiConf) {
	if v := req.TelegramConnection; v != nil {
		SYSTEM_NOTI_TELEGRAM_CONNECTION = *v
	}
	if v := req.TelegramChatId; v != nil {
		SYSTEM_NOTI_TELEGRAM_BOT_CHAT_ID = *v
	}
	if v := req.MessageTopic; v != nil {
		SYSTEM_NOTI_KAFKA_PUBLISHER = *v
	}
	if v := req.Engine; v != nil && *v != "" {
		SYSTEM_NOTI_ENGINE = SystemNotiEngine(*v)
	}
	fmt.Printf("[system-notification] Ready (engine: %s)\n", SYSTEM_NOTI_ENGINE)
}
