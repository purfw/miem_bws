package audit

import (
	"context"
	"github.com/confhub/shared/httputil"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"net/http"
)

type LoginRequest struct {
	EventID      uuid.UUID  `json:"event_id"`
	EventType    string     `json:"event_type"`
	ActorID      *uuid.UUID `json:"actor_id"`
	UsernameHash string     `json:"username_hash"`
}

func LoginHandler(pool *pgxpool.Pool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var in LoginRequest
		if err := httputil.DecodeJSON(r, &in); err != nil {
			httputil.WriteError(w, r, err)
			return
		}
		allowed := map[string]bool{"admin.login": true, "admin.login_failed": true, "admin.login_limited": true,
			"admin.logout": true, "admin.invitation_created": true, "admin.access_change_requested": true, "admin.activation_requested": true}
		if in.EventID == uuid.Nil || len(in.UsernameHash) != 64 || !allowed[in.EventType] {
			httputil.WriteError(w, r, httputil.ErrValidation)
			return
		}
		tx, err := pool.Begin(r.Context())
		if err != nil {
			httputil.WriteError(w, r, err)
			return
		}
		defer tx.Rollback(context.WithoutCancel(r.Context()))
		err = Insert(r.Context(), tx, Entry{EventID: in.EventID, EventType: in.EventType, ActorID: in.ActorID, EntityType: "administrator", Payload: map[string]any{"username_hash": in.UsernameHash}})
		if err == nil {
			err = tx.Commit(r.Context())
		}
		if err != nil {
			httputil.WriteError(w, r, err)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}
