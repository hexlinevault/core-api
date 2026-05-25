package utils

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"time"

	"bytes"
	"html/template"

	"github.com/hexlinevault/core-api/bootstrap"

	"github.com/IBM/sarama"
	"github.com/go-telegram/bot"
	"github.com/go-telegram/bot/models"
)

type (
	SystemNoti struct {
		Environment string    `bson:"environment" json:"environment"`
		Type        string    `bson:"type" json:"type"`
		Time        time.Time `bson:"time" json:"time"`
		Message     string    `bson:"message" json:"message"`
		Error       string    `bson:"error" json:"error"`
		CodeLine    string    `bson:"codeline" json:"codeline"`
	}

	NotiType string
)

var (
	NotiTypeNotification NotiType = "notification"
	NotiTypeError        NotiType = "error"
	NotiTypeWarning      NotiType = "warning"
)

func (e *SystemNoti) CollectionName() string {
	return "system_notifications"
}

// TriggerSystemNoti will log high level message send notification to developer
func TriggerSystemNoti(ct context.Context, notiType NotiType, message string, err error) {
	codeLine := ""
	if _, file, no, ok := runtime.Caller(1); ok {
		codeLine = fmt.Sprintf("called from %s, line: %d", file, no)
	}
	bootstrap.Logger(ct).Error(message, err)
	errMsg := ""
	if err != nil {
		errMsg = err.Error()
	}
	noti := &SystemNoti{
		Environment: os.Getenv("APP_ENV"),
		Type:        string(notiType),
		Message:     message,
		Error:       errMsg,
		Time:        time.Now(),
		CodeLine:    codeLine,
	}
	if err := bootstrap.Kafka.Publish(ct, bootstrap.SYSTEM_NOTI_KAFKA_PUBLISHER, noti); err != nil {
		bootstrap.Logger(ct).Error("trigger noti error", err)
	}
}

// SystemNotiSubscriber subscribe system notification need to InitSystemNoti first
func SystemNotiSubscriber() func(msg *sarama.ConsumerMessage, noti *SystemNoti) error {
	if bootstrap.SYSTEM_NOTI_TELEGRAM_BOT_CHAT_ID == "" {
		panic("[system-noti] telegram chat id is require")
	}
	tg := new(bootstrap.TelegramBot)
	if bootstrap.SYSTEM_NOTI_TELEGRAM_CONNECTION == "" || tg.Bot(bootstrap.SYSTEM_NOTI_TELEGRAM_CONNECTION) == nil {
		panic("[system-noti] telegram connection is required")
	}
	return func(msg *sarama.ConsumerMessage, noti *SystemNoti) error {
		{
			errorBlock := ""
			if noti.Error != "" {
				errorBlock = `
<pre><code>{{ .Error }}</code></pre>
`
			}
			codeBlock := ""
			if noti.Type != string(NotiTypeNotification) {
				codeBlock = `
<pre><code>{{ .CodeLine }}</code></pre>`
			}
			chatID := bootstrap.SYSTEM_NOTI_TELEGRAM_BOT_CHAT_ID // Replace with your chat ID
			msgText := []byte(`<b>System {{ .Type }}</b>
<b>[{{ .Environment }}] {{ .Time }}</b>
<b>{{ .Message }}</b>
` + errorBlock + `
` + codeBlock + `
`)
			t := template.Must(template.New("notiMsg").Parse(string(msgText)))
			b := new(bytes.Buffer)
			t.Execute(b, noti)

			// Create a new message
			msg := &bot.SendMessageParams{
				ChatID:    chatID,
				Text:      b.String(),
				ParseMode: models.ParseModeHTML,
			}

			// Send the message
			ctx := context.Background()
			_, err := new(bootstrap.TelegramBot).Bot(bootstrap.SYSTEM_NOTI_TELEGRAM_CONNECTION).SendMessage(ctx, msg)
			if err != nil {
				bootstrap.Logger(ctx).Error("[telegram] send message error ", err)
			}
		}
		return nil
	}
}
