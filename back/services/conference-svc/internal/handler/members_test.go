package handler

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/confhub/shared/jwtauth"
	"github.com/confhub/shared/testutil"

	"github.com/confhub/conference-svc/internal/identity"
	"github.com/confhub/conference-svc/internal/repository"
)

type fakeUsers map[string]identity.User

func (f fakeUsers) ByEmail(_ context.Context, email string) (identity.User, error) {
	for _, u := range f {
		if strings.EqualFold(u.Email, email) {
			return u, nil
		}
	}
	return identity.User{}, identity.ErrNotFound
}

func (f fakeUsers) ByID(_ context.Context, id string) (identity.User, error) {
	if u, ok := f[id]; ok {
		return u, nil
	}
	return identity.User{}, identity.ErrNotFound
}

func TestMembersOwnerManagesTeam(t *testing.T) {
	pool := testutil.Database(t)
	issuer, verifier := testJWT(t)
	ctx := context.Background()
	owner, member, plain := uuid.New(), uuid.New(), uuid.New()
	conference := uuid.New()
	if _, err := pool.Exec(ctx, `INSERT INTO conference_svc.conferences(id,title,slug,start_date,end_date,created_by)
		VALUES($1,'Team',$2,$3,$3,$4)`, conference, uuid.NewString(), time.Now().AddDate(0, 3, 0), owner); err != nil {
		t.Fatal(err)
	}
	users := fakeUsers{
		member.String(): {ID: member.String(), Email: "member@example.test", FirstName: "Мария", LastName: "Иванова", Role: "organizer_member", Active: true},
		plain.String():  {ID: plain.String(), Email: "user@example.test", Role: "user", Active: true},
	}
	repo := repository.NewConferences(pool)
	r := chi.NewRouter()
	r.Use(verifier.Middleware(true))
	r.Route("/api/v1/conferences", NewMembers(repo, users).Routes)
	call := func(user uuid.UUID, role jwtauth.Role, method, path, body string) int {
		req := httptest.NewRequest(method, path, strings.NewReader(body))
		token, _, err := issuer.Issue(user.String(), role, "x@example.test", "X", true)
		if err != nil {
			t.Fatal(err)
		}
		req.Header.Set("Authorization", "Bearer "+token)
		res := httptest.NewRecorder()
		r.ServeHTTP(res, req)
		return res.Code
	}
	base := "/api/v1/conferences/" + conference.String() + "/members"

	if code := call(owner, jwtauth.RoleVerifiedOrganizer, "PUT", base, `{"email":"user@example.test","permissions":["edit"]}`); code != http.StatusUnprocessableEntity && code != http.StatusBadRequest {
		t.Fatalf("plain user must be rejected, got %d", code)
	}
	if code := call(owner, jwtauth.RoleVerifiedOrganizer, "PUT", base, `{"email":"MEMBER@example.test","permissions":["edit","schedule"]}`); code != http.StatusOK {
		t.Fatalf("add member: %d", code)
	}
	if allowed, err := repo.CanManagePermission(ctx, conference, member, "schedule"); err != nil || !allowed {
		t.Fatalf("member must get schedule permission: %v %v", allowed, err)
	}
	if code := call(member, jwtauth.RoleOrganizerMember, "GET", base, ""); code != http.StatusForbidden {
		t.Fatalf("members must not manage the team, got %d", code)
	}
	if code := call(owner, jwtauth.RoleVerifiedOrganizer, "DELETE", base+"/"+member.String(), ""); code != http.StatusNoContent {
		t.Fatalf("remove: %d", code)
	}
	if allowed, _ := repo.CanManagePermission(ctx, conference, member, ""); allowed {
		t.Fatal("removed member keeps access")
	}
}
