package bootstrap

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"html/template"
	"os"
	"strconv"

	systementities "github.com/hexlinevault/core-api.git/entities/systems"

	"gopkg.in/gomail.v2"
)

type (
	// Mailer mailer
	Mailer struct {
	}
)

var mailer *gomail.Dialer

// CreateMailerConnection create mailer connection
func CreateMailerConnection() {
	port, err := strconv.Atoi(os.Getenv("MAIL_PORT"))
	if err != nil {
		Logger(context.Background()).WithError(err).WithField("component", "mailer").Fatal("smtp port is invalid")
		return
	}
	d := gomail.NewDialer(
		os.Getenv("MAIL_SMTP"),
		port,
		os.Getenv("MAIL_USERNAME"),
		os.Getenv("MAIL_PASSWORD"),
	)
	if os.Getenv("MAIL_INSECURE") == "true" {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: true}
	} else if os.Getenv("MAIL_INSECURE") == "false" {
		d.TLSConfig = &tls.Config{InsecureSkipVerify: false}
	}
	mailer = d
}

// Mail get mailer fn
func (ctl *Mailer) Mail() *gomail.Dialer {
	return mailer
}

func buildMailMessage(mail *systementities.Mail, body string) *gomail.Message {
	m := gomail.NewMessage()
	m.SetAddressHeader("From", mail.From, mail.Sender)
	if len(mail.To) == 1 {
		m.SetAddressHeader("To", mail.To[0], mail.Receiver)
	} else {
		m.SetHeader("To", mail.To...)
	}
	if len(mail.Cc) > 0 {
		m.SetHeader("Cc", mail.Cc...)
	}
	if len(mail.Bcc) > 0 {
		m.SetHeader("Bcc", mail.Bcc...)
	}
	m.SetHeader("Subject", mail.Subject)
	m.SetBody("text/html", body)
	for _, i := range mail.Attach {
		m.Attach(i)
	}
	return m
}

// Send send mail with optional template rendering
func (ctl *Mailer) Send(ct context.Context, mail *systementities.Mail, temp ...*template.Template) error {
	var body bytes.Buffer
	if len(temp) > 0 {
		if len(temp) > 1 {
			return fmt.Errorf("template must be only one")
		}
		if err := temp[0].Execute(&body, mail); err != nil {
			return err
		}
	}
	return ctl.Mail().DialAndSend(buildMailMessage(mail, body.String()))
}

// SendMail send mail with pre-built body
func (ctl *Mailer) SendMail(ct context.Context, mail *systementities.Mail) error {
	return ctl.Mail().DialAndSend(buildMailMessage(mail, mail.Body))
}
