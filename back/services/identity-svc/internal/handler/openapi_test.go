package handler

import (
	"github.com/confhub/shared/jwtauth"
	"github.com/confhub/shared/openapi"
	"github.com/confhub/shared/operations"
	"github.com/go-chi/chi/v5"
	"testing"
)

func TestAllIdentityRoutesHaveContracts(t *testing.T) {
	r := chi.NewRouter()
	r.Route("/api/v1/auth", func(r chi.Router) {
		NewAuth(nil, 0, false, jwtauth.JWKSet{}, nil, "").Routes(r)
		NewAuth(nil, 0, false, jwtauth.JWKSet{}, nil, "").AuthenticatedRoutes(r)
		NewMe(nil, nil).Routes(r)
	})
	r.Route("/internal", func(r chi.Router) {
		NewInternal(nil, nil).Routes(r)
		operations.New(nil, "identity_svc", nil).Routes(r)
	})
	if err := openapi.ValidateRoutes(r, "identity-svc", APIModels()); err != nil {
		t.Fatal(err)
	}
}
