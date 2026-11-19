package natsx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strconv"
	"time"

	"github.com/nats-io/nats.go"
)

const EventStream = "CONFHUB_EVENTS"

var eventSubjects = []string{
	"user.>",
	"application.>",
	"review.>",
	"conference.>",
	"schedule.>",
	"registration.>",
	"document.>",
	"verification.>",
}

// streamReplicas reads NATS_STREAM_REPLICAS: 1 for a single server, 3 for a
// JetStream cluster that must survive the loss of one node.
func streamReplicas() int {
	n, err := strconv.Atoi(os.Getenv("NATS_STREAM_REPLICAS"))
	if err != nil || n < 1 || n > 5 {
		return 1
	}
	return n
}

func Connect(url, name string) (*nats.Conn, error) {
	nc, err := nats.Connect(url,
		nats.Name(name),
		nats.MaxReconnects(-1),
		nats.ReconnectWait(2*time.Second),
		nats.RetryOnFailedConnect(true),
		nats.Timeout(5*time.Second),
		nats.PingInterval(20*time.Second),
		nats.MaxPingsOutstanding(3),
		nats.DisconnectErrHandler(func(_ *nats.Conn, err error) {
			slog.Warn("nats disconnected", "name", name, "err", err)
		}),
		nats.ReconnectHandler(func(c *nats.Conn) {
			slog.Info("nats reconnected", "name", name, "url", c.ConnectedUrl())
		}),
		nats.ErrorHandler(func(_ *nats.Conn, sub *nats.Subscription, err error) {
			s := ""
			if sub != nil {
				s = sub.Subject
			}
			slog.Error("nats async error", "name", name, "subject", s, "err", err)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("nats connect: %w", err)
	}
	return nc, nil
}

// EnsureEventStream creates the shared stream; concurrent calls are safe.
func EnsureEventStream(nc *nats.Conn) (nats.JetStreamContext, error) {
	js, err := nc.JetStream()
	if err != nil {
		return nil, fmt.Errorf("jetstream context: %w", err)
	}
	if _, err = js.StreamInfo(EventStream); err == nil {
		info, err := js.StreamInfo(EventStream)
		if err != nil {
			return nil, err
		}
		found := false
		for _, subject := range info.Config.Subjects {
			if subject == "verification.>" {
				found = true
			}
		}
		if !found {
			info.Config.Subjects = append(info.Config.Subjects, "verification.>")
		}
		// A stream created on a single server is scaled out once the cluster is available.
		scale := info.Config.Replicas < streamReplicas()
		if scale {
			info.Config.Replicas = streamReplicas()
		}
		if !found || scale {
			if _, err := js.UpdateStream(&info.Config); err != nil {
				return nil, err
			}
		}
		return js, nil
	} else if !errors.Is(err, nats.ErrStreamNotFound) {
		return nil, fmt.Errorf("jetstream stream info: %w", err)
	}
	_, err = js.AddStream(&nats.StreamConfig{
		Name:        EventStream,
		Description: "ConfHub transactional domain events",
		Subjects:    eventSubjects,
		Retention:   nats.LimitsPolicy,
		Storage:     nats.FileStorage,
		Discard:     nats.DiscardOld,
		MaxAge:      400 * 24 * time.Hour,
		Duplicates:  10 * time.Minute,
		Replicas:    streamReplicas(),
	})
	if err != nil && !errors.Is(err, nats.ErrStreamNameAlreadyInUse) {
		// Another service may have created the stream first.
		if _, infoErr := js.StreamInfo(EventStream); infoErr != nil {
			return nil, fmt.Errorf("create jetstream stream: %w", err)
		}
	}
	return js, nil
}

// SubscribeDurable registers a queue consumer with explicit ACK. Without a
// FailureStore the message is redelivered until the handler succeeds; with one
// it is quarantined in PostgreSQL after the configured number of attempts.
// Errors wrapping ErrPermanent are quarantined at once, or terminated when no
// FailureStore is configured.
func SubscribeDurable(nc *nats.Conn, subject, durable string, handler func(*nats.Msg) error, stores ...*FailureStore) error {
	var store *FailureStore
	if len(stores) > 0 {
		store = stores[0]
	}
	if store != nil {
		store.register(durable, handler)
	}
	if err := subscribeDurable(nc, subject, durable, handler, store); err == nil {
		return nil
	}
	go func() {
		for !nc.IsClosed() {
			time.Sleep(2 * time.Second)
			if !nc.IsConnected() {
				continue
			}
			if err := subscribeDurable(nc, subject, durable, handler, store); err == nil {
				return
			} else {
				slog.Error("retry durable subscribe", "subject", subject, "err", err)
			}
		}
	}()
	return nil
}

func subscribeDurable(nc *nats.Conn, subject, durable string, handler func(*nats.Msg) error, store *FailureStore) error {
	if !nc.IsConnected() {
		return nats.ErrDisconnected
	}
	js, err := EnsureEventStream(nc)
	if err != nil {
		return err
	}
	if info, err := js.ConsumerInfo(EventStream, durable); err == nil && info.Config.MaxDeliver != -1 {
		info.Config.MaxDeliver = -1
		if _, err := js.UpdateConsumer(EventStream, &info.Config); err != nil {
			return err
		}
	} else if err != nil && !errors.Is(err, nats.ErrConsumerNotFound) {
		return err
	}
	_, err = js.QueueSubscribe(subject, durable, func(msg *nats.Msg) {
		var attempts uint64
		if meta, err := msg.Metadata(); err == nil {
			attempts = meta.NumDelivered
		}
		var save func() error
		if store != nil {
			save = func() error {
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				defer cancel()
				return store.save(ctx, durable, msg, attempts)
			}
		}
		if err := deliveryResult(func() error { return handler(msg) }, attempts, save); err != nil {
			if errors.Is(err, ErrPermanent) {
				slog.Error("durable event dropped", "subject", subject, "durable", durable, "err", err)
				if termErr := msg.Term(); termErr != nil {
					slog.Error("nats term failed", "subject", subject, "durable", durable, "err", termErr)
				}
				return
			}
			slog.Error("durable event handler failed", "subject", subject, "durable", durable, "err", err)
			if nakErr := msg.NakWithDelay(5 * time.Second); nakErr != nil {
				slog.Error("nats nak failed", "subject", subject, "durable", durable, "err", nakErr)
			}
			return
		}
		if err := msg.Ack(); err != nil && !errors.Is(err, context.Canceled) {
			slog.Error("nats ack failed", "subject", subject, "durable", durable, "err", err)
		}
	},
		nats.Durable(durable),
		nats.ManualAck(),
		nats.AckExplicit(),
		nats.DeliverAll(),
		nats.AckWait(60*time.Second),
		nats.MaxDeliver(-1),
	)
	if err != nil {
		return fmt.Errorf("durable subscribe %s: %w", subject, err)
	}
	return nil
}
