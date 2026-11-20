package repository

import (
	"context"
	"time"

	"github.com/google/uuid"
)

// Member permissions; "edit" covers the event card and its content,
// "schedule" the programme, "participants" registrations.
var MemberPermissions = []string{"edit", "schedule", "participants"}

type Member struct {
	UserID      uuid.UUID
	Permissions []string
	AddedBy     uuid.UUID
	CreatedAt   time.Time
}

// IsOwner reports whether the user created the conference.
func (r *Conferences) IsOwner(ctx context.Context, conferenceID, userID uuid.UUID) (bool, error) {
	var owner bool
	err := r.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM conference_svc.conferences
		WHERE id=$1 AND created_by=$2 AND deleted_at IS NULL)`, conferenceID, userID).Scan(&owner)
	return owner, err
}

func (r *Conferences) Members(ctx context.Context, conferenceID uuid.UUID) ([]Member, error) {
	rows, err := r.pool.Query(ctx, `SELECT user_id,permissions,added_by,created_at FROM conference_svc.conference_members
		WHERE conference_id=$1 ORDER BY created_at, user_id`, conferenceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Member
	for rows.Next() {
		var m Member
		if err := rows.Scan(&m.UserID, &m.Permissions, &m.AddedBy, &m.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SaveMember adds a member or replaces the permissions of an existing one.
func (r *Conferences) SaveMember(ctx context.Context, conferenceID, userID, addedBy uuid.UUID, permissions []string) error {
	_, err := r.pool.Exec(ctx, `INSERT INTO conference_svc.conference_members(conference_id,user_id,permissions,added_by)
		VALUES ($1,$2,$3,$4) ON CONFLICT (conference_id,user_id) DO UPDATE SET permissions=excluded.permissions`,
		conferenceID, userID, permissions, addedBy)
	return err
}

func (r *Conferences) RemoveMember(ctx context.Context, conferenceID, userID uuid.UUID) (bool, error) {
	tag, err := r.pool.Exec(ctx, `DELETE FROM conference_svc.conference_members WHERE conference_id=$1 AND user_id=$2`, conferenceID, userID)
	return tag.RowsAffected() > 0, err
}
