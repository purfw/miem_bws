package handler

import (
	"github.com/confhub/conference-svc/internal/domain"
	"github.com/confhub/shared/openapi"
	"time"
)

func APIModels() map[string]openapi.Models {
	q := openapi.Query
	page := openapi.PageParameters()
	listParameters := append(append([]openapi.Parameter{}, page...),
		q("q", "string", "Full-text query"), q("searchText", "string", "Alias for q"),
		q("topics", "string", "Comma-separated topic filters"), q("format", "string", "Comma-separated offline, hybrid or online formats"),
		q("city", "string", "Comma-separated cities"), q("sort", "string", "start_date, title or participants"),
		q("tab", "string", "active, upcoming or past"), q("status", "string", "Comma-separated public statuses"),
		q("from_date", "string", "Inclusive start date YYYY-MM-DD"), q("to_date", "string", "Inclusive end date YYYY-MM-DD"), q("include", "string", "hero"))
	identity := map[string]any{"id": "", "slug": "", "status": ""}
	registration := map[string]any{"data": registrationDTO{}}
	session := sessionDTO(&domain.ScheduleSlot{}, false, nil)
	status := map[string]any{"id": "", "title": "", "slug": "", "status": "", "topics": []string{}, "format": "", "city": (*string)(nil), "start_date": "", "registration_deadline": (*time.Time)(nil)}
	return map[string]openapi.Models{
		"(*Public).list":                   {Response: listResp{}, Parameters: listParameters, OptionalAuth: true},
		"(*Public).detail":                 {Response: detailResp{}, OptionalAuth: true},
		"(*Public).home":                   {Response: homeResp{}, Parameters: []openapi.Parameter{q("stats", "string", "counts returns only conference and participant counts, without acceptance statistics")}},
		"(*Public).autocomplete":           {Response: map[string]any{"data": map[string]any{"suggestions": openapi.Array(map[string]any{"text": "", "type": "", "slug": ""})}}, Parameters: []openapi.Parameter{q("q", "string", "Query of at least two characters"), q("limit", "integer", "Maximum suggestions, defaults to 5 and capped at 10")}, OptionalAuth: true},
		"(*Public).calendar":               {Response: "", ContentType: "text/calendar", OptionalAuth: true},
		"(*Public).program":                {Response: programResp{}},
		"(*Public).talks":                  {Response: talksResp{}, Parameters: append(append([]openapi.Parameter{}, page...), q("q", "string", "Title or speaker search"), q("searchText", "string", "Alias for q"), q("section_id", "string", "Section UUID"), q("day", "string", "Schedule date YYYY-MM-DD"))},
		"(*Public).organizers":             {Response: organizersResp{}},
		"(*Public).partners":               {Response: map[string]any{"data": []partnerDTO{}}},
		"(*Public).addFavorite":            {Status: 204, Auth: true},
		"(*Public).removeFavorite":         {Status: 204, Auth: true},
		"(*Public).listFavorites":          {Response: favoritesResp{}, Parameters: page, Auth: true},
		"(*Organizer).create":              {Request: organizerCreateReq{}, Response: identity, Status: 201, Auth: true, Description: "Requires a verified organizer role. start_date must be on or after " + domain.MinimumStartDate + "."},
		"(*Organizer).update":              {Request: updateReq{}, Response: identity, Auth: true, Description: "Only the owner or an authorized organization member may update the event. The resulting start_date must be on or after " + domain.MinimumStartDate + ", including partial edits of older events."},
		"(*Organizer).cancel":              {Status: 204, Auth: true},
		"(*Organizer).mine":                {Response: map[string]any{"data": []cardDTO{}}, Auth: true},
		"(*Organizer).listManagedSessions": {Response: map[string]any{"data": openapi.Array(session)}, Auth: true},
		"(*Organizer).createSession":       {Request: sessionReq{}, Response: session, Status: 201, Auth: true},
		"(*Organizer).updateSession":       {Request: sessionReq{}, Response: session, Auth: true},
		"(*Organizer).deleteSession":       {Status: 204, Auth: true},
		"(*Organizer).setSessionLink":      {Request: sessionLinkReq{}, Response: map[string]any{"data": map[string]any{"id": "", "has_join_url": true, "join_url_public_at": (*time.Time)(nil)}}, Auth: true},
		"(*Organizer).joinSession":         {Status: 302, OtherResponses: map[int]openapi.Models{200: {Response: map[string]any{"url": ""}}}, Auth: true, Parameters: []openapi.Parameter{{Name: "Accept", In: "header", Type: "string", Description: "application/json returns the URL instead of redirecting"}}, Description: "Confirmed registration and current account authorization are checked without a stale cache."},
		"(*Internal).create":               {Request: createReq{}, Response: map[string]any{"id": "", "slug": ""}, Status: 201, Description: "start_date must be on or after " + domain.MinimumStartDate + "."},
		"(*Internal).update":               {Request: updateReq{}, Response: map[string]any{"id": "", "status": ""}, Description: "The resulting start_date must be on or after " + domain.MinimumStartDate + ", including partial edits of older events."},
		"(*Internal).softDelete":           {Status: 204},
		"(*Internal).statusFallback":       {Response: statusDTO{}},
		"(*Internal).checkSection":         {Response: map[string]any{"belongs": true}},
		"(*Internal).replaceSchedule":      {Request: scheduleReq{}, Status: 204},
		"(*Internal).export":               {Response: exportResp{}, Parameters: []openapi.Parameter{q("cursor", "string", "Opaque pagination cursor"), q("limit", "integer", "Page size; defaults to 500 and capped at 500")}},
		"(*Internal).reminderCandidates":   {Response: map[string]any{"items": openapi.Array(status), "next_cursor": ""}, Parameters: []openapi.Parameter{q("cursor", "string", "Previous page's final conference UUID")}},
		"(*Internal).listRegistrations":    {Response: map[string]any{"data": openapi.Array(map[string]any{"id": "", "user_id": "", "user_email": "", "user_name": "", "status": "", "role_at_conf": "", "registered_at": ""})}, Parameters: []openapi.Parameter{{Name: "conference_id", In: "query", Required: true, Type: "string", Format: "uuid"}, q("status", "string", "Optional registration status")}},
		"(*Internal).checkRegistration":    {Response: map[string]any{"data": registrationCheckDTO{}}, Parameters: []openapi.Parameter{{Name: "conference_id", In: "query", Required: true, Type: "string", Format: "uuid"}, {Name: "user_id", In: "query", Required: true, Type: "string", Format: "uuid"}}},
		"(*Registrations).register":        {Request: registerReq{}, Response: registration, Status: 201, Auth: true},
		"(*Registrations).current":         {Response: map[string]any{"data": (*registrationDTO)(nil)}, Auth: true, Description: "Returns only the current user's registration for this event, including cancelled status; null means no registration exists."},
		"(*Registrations).cancel":          {Response: registration, Auth: true},
		"(*Registrations).listMine":        {Response: map[string]any{"data": []registrationDTO{}, "pagination": paginationDTO{}}, Parameters: append(append([]openapi.Parameter{}, page...), q("status", "string", "confirmed, waitlisted or cancelled")), Auth: true},
	}
}
