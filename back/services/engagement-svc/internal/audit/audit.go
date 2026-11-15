// Package audit writes to an append-only table protected by PostgreSQL rules.
package audit

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Entry struct {
	EventID    uuid.UUID
	EventType  string
	ActorID    *uuid.UUID
	EntityType string
	EntityID   *uuid.UUID
	Payload    any
}

func Insert(ctx context.Context, tx pgx.Tx, e Entry) error {
	pl, err := sanitizedPayload(e.Payload)
	if err != nil {
		return err
	}
	_, err = tx.Exec(ctx, `INSERT INTO engagement_svc.audit_log
		(event_id, event_type, actor_id, entity_type, entity_id, payload)
		SELECT $1,$2,$3,$4,$5,$6::jsonb
		WHERE NOT EXISTS (SELECT 1 FROM engagement_svc.audit_log WHERE event_id=$1)`,
		e.EventID, e.EventType, e.ActorID, e.EntityType, e.EntityID, pl)
	return err
}

func sanitizedPayload(payload any) ([]byte, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return nil, err
	}
	var value any
	if err := json.Unmarshal(raw, &value); err != nil {
		return nil, err
	}
	redact(value)
	return json.Marshal(value)
}

func redact(value any) {
	switch value := value.(type) {
	case map[string]any:
		for key, child := range value {
			normalized := strings.ToLower(strings.NewReplacer("_", "", "-", "").Replace(key))
			if strings.Contains(normalized, "token") || strings.Contains(normalized, "password") ||
				strings.Contains(normalized, "secret") || strings.Contains(normalized, "authorization") ||
				strings.Contains(normalized, "downloadurl") || strings.Contains(normalized, "verifyurl") || strings.Contains(normalized, "reseturl") {
				delete(value, key)
				continue
			}
			redact(child)
		}
	case []any:
		for _, child := range value {
			redact(child)
		}
	}
}
