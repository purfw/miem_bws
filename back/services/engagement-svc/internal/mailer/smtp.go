package mailer

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net"
	"net/mail"
	"net/smtp"
	"net/textproto"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
)

func buildMessage(from, to, subject, body string, calendar []byte, id uuid.UUID) ([]byte, error) {
	if strings.ContainsAny(from+to+subject, "\r\n") {
		return nil, errors.New("invalid email header")
	}
	if _, err := mail.ParseAddress(to); err != nil {
		return nil, errors.New("invalid recipient")
	}
	var wire bytes.Buffer
	fmt.Fprintf(&wire, "From: %s\r\nTo: %s\r\nSubject: %s\r\nDate: %s\r\nMessage-ID: <%s@confhub>\r\nMIME-Version: 1.0\r\n", from, to, mime.QEncoding.Encode("UTF-8", subject), time.Now().UTC().Format(time.RFC1123Z), id)
	if len(calendar) == 0 {
		fmt.Fprintf(&wire, "Content-Type: text/html; charset=UTF-8\r\n\r\n%s", body)
		return wire.Bytes(), nil
	}
	mw := multipart.NewWriter(&wire)
	fmt.Fprintf(&wire, "Content-Type: multipart/mixed; boundary=%q\r\n\r\n", mw.Boundary())
	htmlHeaders := textproto.MIMEHeader{}
	htmlHeaders.Set("Content-Type", "text/html; charset=UTF-8")
	htmlPart, err := mw.CreatePart(htmlHeaders)
	if err != nil {
		return nil, err
	}
	if _, err = io.WriteString(htmlPart, body); err != nil {
		return nil, err
	}
	calendarHeaders := textproto.MIMEHeader{}
	calendarHeaders.Set("Content-Type", "text/calendar; charset=UTF-8; method=PUBLISH")
	calendarHeaders.Set("Content-Disposition", `attachment; filename="conference.ics"`)
	calendarPart, err := mw.CreatePart(calendarHeaders)
	if err != nil {
		return nil, err
	}
	if _, err = calendarPart.Write(calendar); err != nil {
		return nil, err
	}
	if err = mw.Close(); err != nil {
		return nil, err
	}
	return wire.Bytes(), nil
}

func (m *Mailer) dispatch(ctx context.Context, to string, wire []byte) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	address := net.JoinHostPort(m.cfg.Host, strconv.Itoa(m.cfg.Port))
	var connection net.Conn
	var err error
	tlsConfig := &tls.Config{ServerName: m.cfg.Host, MinVersion: tls.VersionTLS12}
	implicit := m.cfg.TLSMode == "implicit" || (m.cfg.TLSMode == "" && m.cfg.Port == 465)
	if implicit {
		dialer := tls.Dialer{NetDialer: &net.Dialer{Timeout: 10 * time.Second}, Config: tlsConfig}
		connection, err = dialer.DialContext(ctx, "tcp", address)
	} else {
		connection, err = (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
	}
	if err != nil {
		return err
	}
	defer connection.Close()
	deadline, _ := ctx.Deadline()
	if err = connection.SetDeadline(deadline); err != nil {
		return err
	}
	stop := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stop()
	client, err := smtp.NewClient(connection, m.cfg.Host)
	if err != nil {
		return err
	}
	defer client.Close()
	if !implicit && m.cfg.TLSMode != "none" {
		if supported, _ := client.Extension("STARTTLS"); supported {
			if err = client.StartTLS(tlsConfig); err != nil {
				return err
			}
		} else if m.cfg.TLSMode == "starttls" || m.cfg.User != "" {
			return errors.New("SMTP server does not provide STARTTLS")
		}
	}
	if m.cfg.User != "" {
		if err = client.Auth(smtp.PlainAuth("", m.cfg.User, m.cfg.Password, m.cfg.Host)); err != nil {
			return err
		}
	}
	if err = client.Mail(m.cfg.From); err != nil {
		return err
	}
	recipient, err := mail.ParseAddress(to)
	if err != nil {
		return err
	}
	if err = client.Rcpt(recipient.Address); err != nil {
		return err
	}
	writer, err := client.Data()
	if err != nil {
		return err
	}
	if _, err = writer.Write(wire); err != nil {
		return err
	}
	if err = writer.Close(); err != nil {
		return err
	}
	_ = client.Quit()
	return nil
}
