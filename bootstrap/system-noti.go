package bootstrap

import (
	"fmt"

	"github.com/hexlinevault/core-api/configs"
)

var SYSTEM_NOTI_KAFKA_PUBLISHER = "system-notification-publisher"
var SYSTEM_NOTI_TELEGRAM_CONNECTION = "default"
var SYSTEM_NOTI_TELEGRAM_BOT_CHAT_ID = ""

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
	fmt.Println("[system-notification] Ready")
}
