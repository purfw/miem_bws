package handler

import (
	"github.com/confhub/shared/openapi"
	"github.com/confhub/shared/operations"
	"github.com/go-chi/chi/v5"
	"testing"
)

func TestAllConferenceRoutesHaveContracts(t *testing.T) {
	r := chi.NewRouter()
	public, organizer, registrations, internal := &Public{}, &Organizer{}, &Registrations{}, &Internal{}
	r.Route("/api/v1/conferences", func(r chi.Router) { public.Routes(r); organizer.ConferenceRoutes(r); registrations.ConferenceRoutes(r) })
	r.Route("/api/v1/sessions", organizer.SessionRoutes)
	r.Route("/api/v1/registrations", registrations.RegistrationRoutes)
	r.Route("/api/v1/my", func(r chi.Router) { organizer.MyRoutes(r); registrations.MyRoutes(r) })
	r.Route("/api/v1/home", public.HomeRoutes)
	r.Route("/api/v1/search", public.SearchRoutes)
	r.Route("/api/v1/favorites", public.FavoritesRoutes)
	r.Route("/internal", func(r chi.Router) { internal.Routes(r); operations.New(nil, "conference_svc", nil).Routes(r) })
	if err := openapi.ValidateRoutes(r, "conference-svc", APIModels()); err != nil {
		t.Fatal(err)
	}
}
