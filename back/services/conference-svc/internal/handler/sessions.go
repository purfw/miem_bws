package handler

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/httputil"

	"github.com/confhub/conference-svc/internal/domain"
)

type sessionReq struct {
	SectionID          *string `json:"section_id,omitempty"`
	Title              string  `json:"title"`
	SpeakerName        *string `json:"speaker_name,omitempty"`
	SpeakerAffiliation *string `json:"speaker_affiliation,omitempty"`
	Room               *string `json:"room,omitempty"`
	SlotDate           string  `json:"slot_date"`
	StartTime          string  `json:"start_time"`
	EndTime            string  `json:"end_time"`
	Description        *string `json:"description,omitempty"`
	SessionType        string  `json:"session_type"`
}

func (h *Organizer) listManagedSessions(w http.ResponseWriter, r *http.Request) {
	conferenceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	if _, err := h.requireManage(r, conferenceID, ""); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	slots, err := h.repo.Schedule(r.Context(), conferenceID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	data := make([]map[string]any, 0, len(slots))
	for i := range slots {
		data = append(data, sessionDTO(&slots[i], false, nil))
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": data})
}

func (h *Organizer) createSession(w http.ResponseWriter, r *http.Request) {
	conferenceID, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	if _, err := h.requireManage(r, conferenceID, "schedule"); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var in sessionReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	slot, err := sessionFromReq(in)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	slot.ConferenceID = conferenceID
	if err := h.repo.CreateSession(r.Context(), slot); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusCreated, sessionDTO(slot, false, nil))
}

func sessionFromReq(in sessionReq) (*domain.ScheduleSlot, error) {
	in.Title = strings.TrimSpace(in.Title)
	if in.Title == "" {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "title", Message: "required"}}}
	}
	date, err := time.Parse("2006-01-02", in.SlotDate)
	if err != nil {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "slot_date", Message: "YYYY-MM-DD"}}}
	}
	start, err := time.Parse("15:04", in.StartTime)
	if err != nil {
		start, err = time.Parse("15:04:05", in.StartTime)
	}
	if err != nil {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "start_time", Message: "HH:MM"}}}
	}
	end, err := time.Parse("15:04", in.EndTime)
	if err != nil {
		end, err = time.Parse("15:04:05", in.EndTime)
	}
	if err != nil || !end.After(start) {
		return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "end_time", Message: "must be after start_time"}}}
	}
	var sectionID *uuid.UUID
	if in.SectionID != nil && *in.SectionID != "" {
		parsed, parseErr := uuid.Parse(*in.SectionID)
		if parseErr != nil {
			return nil, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "section_id", Message: "invalid uuid"}}}
		}
		sectionID = &parsed
	}
	if in.SessionType == "" {
		in.SessionType = "talk"
	}
	return &domain.ScheduleSlot{
		SectionID: sectionID, Title: in.Title, SpeakerName: in.SpeakerName,
		SpeakerAff: in.SpeakerAffiliation, Room: in.Room, SlotDate: date,
		StartTime: start.Format("15:04:05"), EndTime: end.Format("15:04:05"),
		Description: in.Description, SessionType: in.SessionType,
	}, nil
}

func sessionDTO(slot *domain.ScheduleSlot, hasJoin bool, publicAt *time.Time) map[string]any {
	return map[string]any{
		"id": slot.ID.String(), "conference_id": slot.ConferenceID.String(), "section_id": slot.SectionID,
		"title": slot.Title, "speaker_name": slot.SpeakerName, "speaker_affiliation": slot.SpeakerAff,
		"room": slot.Room, "slot_date": slot.SlotDate.Format("2006-01-02"), "start_time": slot.StartTime,
		"end_time": slot.EndTime, "description": slot.Description, "session_type": slot.SessionType,
		"has_join_url": hasJoin, "join_url_public_at": publicAt,
	}
}

func (h *Organizer) updateSession(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	current, err := h.repo.GetSessionSecure(r.Context(), id)
	if err != nil {
		httputil.WriteError(w, r, fmt.Errorf("session not found: %w", httputil.ErrNotFound))
		return
	}
	if _, err := h.requireManage(r, current.ConferenceID, "schedule"); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var in sessionReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	slot, err := sessionFromReq(in)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	slot.ID, slot.ConferenceID = id, current.ConferenceID
	if err := h.repo.UpdateSession(r.Context(), slot); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, sessionDTO(slot, len(current.Ciphertext) > 0, current.PublicAt))
}

func (h *Organizer) deleteSession(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	current, err := h.repo.GetSessionSecure(r.Context(), id)
	if err != nil {
		httputil.WriteError(w, r, fmt.Errorf("session not found: %w", httputil.ErrNotFound))
		return
	}
	if _, err := h.requireManage(r, current.ConferenceID, "schedule"); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if err := h.repo.DeleteSession(r.Context(), id); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type sessionLinkReq struct {
	JoinURL  string  `json:"join_url"`
	PublicAt *string `json:"public_at,omitempty"`
}

func (h *Organizer) setSessionLink(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	current, err := h.repo.GetSessionSecure(r.Context(), id)
	if err != nil {
		httputil.WriteError(w, r, fmt.Errorf("session not found: %w", httputil.ErrNotFound))
		return
	}
	if _, err := h.requireManage(r, current.ConferenceID, "schedule"); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	var in sessionLinkReq
	if err := httputil.DecodeJSON(r, &in); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	parsedURL, err := url.ParseRequestURI(in.JoinURL)
	if err != nil || parsedURL.Scheme != "https" || parsedURL.Host == "" {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "join_url", Message: "valid https URL required"}}})
		return
	}
	var publicAt *time.Time
	if in.PublicAt != nil {
		parsed, parseErr := time.Parse(time.RFC3339, *in.PublicAt)
		if parseErr != nil {
			httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "public_at", Message: "RFC3339 required"}}})
			return
		}
		publicAt = &parsed
	}
	ciphertext, nonce, keyVersion, err := h.links.Encrypt(in.JoinURL, []byte(id.String()))
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if err := h.repo.SetSessionLink(r.Context(), id, ciphertext, nonce, keyVersion, publicAt); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	httputil.WriteJSON(w, http.StatusOK, map[string]any{"data": map[string]any{"id": id.String(), "has_join_url": true, "join_url_public_at": publicAt}})
}

func (h *Organizer) joinSession(w http.ResponseWriter, r *http.Request) {
	_, userID, err := organizerClaims(r)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		httputil.WriteError(w, r, &httputil.ValidationError{Fields: []httputil.FieldError{{Field: "id", Message: "invalid uuid"}}})
		return
	}
	session, err := h.repo.GetSessionSecure(r.Context(), id)
	if err != nil || len(session.Ciphertext) == 0 || len(session.Nonce) == 0 {
		httputil.WriteError(w, r, &domain.RegistrationError{Code: "NO_LINK", Message: "join link is not configured", Sentinel: httputil.ErrNotFound})
		return
	}
	if session.PublicAt != nil && time.Now().Before(*session.PublicAt) {
		httputil.WriteError(w, r, &domain.RegistrationError{Code: "NOT_PUBLIC_YET", Message: "join link is not public yet", Sentinel: httputil.ErrForbidden})
		return
	}
	_, confirmed, err := h.registrations.HasConfirmed(r.Context(), session.ConferenceID, userID)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	if !confirmed {
		httputil.WriteError(w, r, &domain.RegistrationError{Code: "NOT_REGISTERED", Message: "confirmed registration required", Sentinel: httputil.ErrForbidden})
		return
	}
	keyVersion := int16(1)
	if session.KeyVersion != nil {
		keyVersion = *session.KeyVersion
	}
	joinURL, err := h.links.Decrypt(session.Ciphertext, session.Nonce, []byte(id.String()), keyVersion)
	if err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	ip := strings.TrimSpace(r.Header.Get("X-Real-IP"))
	if ip == "" {
		ip, _, _ = net.SplitHostPort(r.RemoteAddr)
	}
	hash := sha256.Sum256([]byte(ip))
	if err := h.repo.LogJoinAccess(r.Context(), id, userID, r.Header.Get("X-Request-ID"), hex.EncodeToString(hash[:])); err != nil {
		httputil.WriteError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store, no-cache")
	if strings.Contains(r.Header.Get("Accept"), "application/json") {
		httputil.WriteJSON(w, http.StatusOK, map[string]string{"url": joinURL})
		return
	}
	http.Redirect(w, r, joinURL, http.StatusFound)
}
