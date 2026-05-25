package bootstrap

import (
	"context"
	"fmt"

	"github.com/go-telegram/bot"
)

type (
	// TelegramBot telegram bot
	TelegramBot struct {
	}
)

var telegramBotClients map[string]*bot.Bot = make(map[string]*bot.Bot)

func CreateTelegramBot(botToken string, connectionNames ...string) {
	connectionName := resolveConnectionName(connectionNames)
	if botToken == "" {
		panic("[telegram] environment variable is not set")
	}

	b, err := bot.New(botToken)
	if err != nil {
		panic(fmt.Sprintf("[telegram] %s", err))
	}
	telegramBotClients[connectionName] = b

	Logger(context.Background()).WithField("connection_name", connectionName).WithField("component", "telegram").Info("Telegram bot connected")
}

func (c *TelegramBot) Bot(connectionNames ...string) *bot.Bot {
	connectionName := resolveConnectionName(connectionNames)
	return telegramBotClients[connectionName]
}
