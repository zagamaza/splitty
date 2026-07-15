package rest

import (
	"net/http"
	"testing"

	"github.com/almaznur91/splitty/internal/api"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func sharedRoom() *api.Room {
	return &api.Room{
		ID:         primitive.NewObjectID(),
		Name:       "room",
		Members:    &[]api.User{testUser1, testUser2},
		Operations: &[]api.Operation{},
	}
}

func TestAddAlias_Unauthorized(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(sharedRoom()))
	rec := doRequest(t, s, http.MethodPost, "/api/v1/users/2/aliases", "", `{"alias":"Саня"}`)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401", rec.Code)
	}
}

func TestAddAlias_ForbiddenNoSharedRoom(t *testing.T) {
	// testUser3 не в комнате с testUser1
	ur := newFakeUserRepo(testUser1, testUser2, testUser3)
	s := newTestServer(Config{}, ur, newFakeRoomRepo(sharedRoom()))
	rec := doRequest(t, s, http.MethodPost, "/api/v1/users/3/aliases",
		mustToken(t, s, testUser1.ID), `{"alias":"Гоша"}`)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403", rec.Code)
	}
}

func TestAddAlias_HappyPathAndNormalization(t *testing.T) {
	ur := newFakeUserRepo(testUser1, testUser2)
	s := newTestServer(Config{}, ur, newFakeRoomRepo(sharedRoom()))
	rec := doRequest(t, s, http.MethodPost, "/api/v1/users/2/aliases",
		mustToken(t, s, testUser1.ID), `{"alias":"  Саня  "}`)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want 204, body %s", rec.Code, rec.Body.String())
	}
	u, _ := ur.FindById(nil, 2)
	if len(u.Aliases) != 1 || u.Aliases[0] != "саня" {
		t.Fatalf("алиас не нормализован/не сохранён: %+v", u.Aliases)
	}
}

func TestAddAlias_Idempotent(t *testing.T) {
	ur := newFakeUserRepo(testUser1, testUser2)
	s := newTestServer(Config{}, ur, newFakeRoomRepo(sharedRoom()))
	tok := mustToken(t, s, testUser1.ID)
	doRequest(t, s, http.MethodPost, "/api/v1/users/2/aliases", tok, `{"alias":"саня"}`)
	doRequest(t, s, http.MethodPost, "/api/v1/users/2/aliases", tok, `{"alias":"саня"}`)
	u, _ := ur.FindById(nil, 2)
	if len(u.Aliases) != 1 {
		t.Fatalf("дубль алиаса: %+v", u.Aliases)
	}
}

func TestAddAlias_EmptyRejected(t *testing.T) {
	s := newTestServer(Config{}, newFakeUserRepo(testUser1, testUser2), newFakeRoomRepo(sharedRoom()))
	rec := doRequest(t, s, http.MethodPost, "/api/v1/users/2/aliases",
		mustToken(t, s, testUser1.ID), `{"alias":"   "}`)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", rec.Code)
	}
}
