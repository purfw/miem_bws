package handler

import (
	"github.com/confhub/shared/openapi"
	"github.com/confhub/shared/operations"
	"github.com/go-chi/chi/v5"
	"testing"
)

func TestAllApplicationRoutesHaveContracts(t *testing.T) {
	r := chi.NewRouter()
	apps := NewApplications(nil)
	r.Route("/api/v1", func(r chi.Router) { apps.OpenRoutes(r); apps.PublicRoutes(r); r.Route("/files", NewFiles(apps).Routes) })
	r.Route("/internal", func(r chi.Router) { NewInternal(nil).Routes(r); operations.New(nil, "application_svc", nil).Routes(r) })
	if err := openapi.ValidateRoutes(r, "application-svc", APIModels()); err != nil {
		t.Fatal(err)
	}
}
