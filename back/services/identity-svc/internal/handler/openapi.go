package handler

import (
	"github.com/confhub/identity-svc/internal/service"
	"github.com/confhub/shared/jwtauth"
	"github.com/confhub/shared/openapi"
)

func APIModels() map[string]openapi.Models {
	return map[string]openapi.Models{
		"(*Auth).register":                {Request: service.RegisterInput{}, Response: registerResp{}, Status: 201},
		"(*Auth).login":                   {Request: loginReq{}, Response: loginResp{}},
		"(*Auth).refresh":                 {Response: loginResp{}, CookieAuth: true},
		"(*Auth).logout":                  {Status: 204, Description: "Invalidates the refresh token from the HttpOnly refresh_token cookie."},
		"(*Auth).resendVerification":      {Status: 202, Auth: true, Description: "Sends verification only to the authenticated account's server-held email address. Request body fields are ignored. Limited to one attempt per 60 seconds and three verification messages per hour; Retry-After reports the remaining cooldown."},
		"(*Auth).verifyEmail":             {Status: 204, Parameters: []openapi.Parameter{{Name: "token", In: "query", Required: true, Type: "string", Format: "uuid"}}},
		"(*Auth).jwkSet":                  {Response: jwtauth.JWKSet{}},
		"(*Auth).providers":               {Response: map[string]any{"orcid": false}},
		"(*Auth).orcidStart":              {Status: 302, Description: "Starts OAuth with a server-held PKCE verifier. Returns 503 if ORCID credentials are not configured."},
		"(*Auth).orcidCallback":           {Status: 302, Parameters: []openapi.Parameter{openapi.Query("code", "string", "Authorization code"), openapi.Query("state", "string", "Single-use signed OAuth state"), openapi.Query("error", "string", "Provider error")}},
		"(*Auth).forgotPassword":          {Request: forgotReq{}},
		"(*Auth).resetPassword":           {Request: resetReq{}},
		"(*Me).get":                       {Response: userDTO{}, Auth: true},
		"(*Me).update":                    {Request: service.ProfileInput{}, Response: userDTO{}, Auth: true},
		"(*Me).changePassword":            {Request: changePasswordReq{}, Status: 204, Auth: true},
		"(*Me).delete":                    {Request: deleteAccountReq{}, Status: 204, Auth: true},
		"(*Internal).getUser":             {Response: userDTO{}},
		"(*Internal).listUsers":           {Response: adminUsersResponse{}, Parameters: []openapi.Parameter{openapi.Query("q", "string", "Email or name"), openapi.Query("page", "integer", "Page number")}},
		"(*Internal).getAuthState":        {Response: jwtauth.AuthState{}},
		"(*Internal).getResearchIdentity": {Response: map[string]any{"provider": "", "identifier": "", "display_name": "", "publication_count": 0}},
		"(*Internal).setActive":           {Request: setActiveReq{}, Status: 204},
		"(*Internal).updateRole":          {Request: updateRoleReq{}, Status: 204},
	}
}
