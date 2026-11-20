package main

import (
	"github.com/confhub/engagement-svc/internal/audit"
	"github.com/confhub/engagement-svc/internal/subscriptions"
	"github.com/confhub/engagement-svc/internal/support"
	"github.com/confhub/shared/openapi"
	"github.com/confhub/shared/operations"
	"github.com/go-chi/chi/v5"
	"testing"
)

func TestAllEngagementRoutesHaveContracts(t *testing.T) {
	r := chi.NewRouter()
	h := subscriptions.New(nil)
	r.Route("/api/v1/subscriptions", func(r chi.Router) { h.AuthRoutes(r); h.OpenRoutes(r) })
	r.Post("/api/v1/support/messages", support.New(nil, nil, "").Create)
	r.Route("/internal", func(r chi.Router) {
		operations.New(nil, "engagement_svc", nil).Routes(r)
		r.Post("/admin/auth-audit", audit.LoginHandler(nil))
		support.New(nil, nil, "").AdminRoutes(r)
	})
	models := subscriptions.APIModels()
	for name, model := range support.APIModels() {
		models[name] = model
	}
	models["POST /internal/admin/auth-audit"] = openapi.Models{Request: audit.LoginRequest{}, Status: 204}
	if err := openapi.ValidateRoutes(r, "engagement-svc", models); err != nil {
		t.Fatal(err)
	}
}
