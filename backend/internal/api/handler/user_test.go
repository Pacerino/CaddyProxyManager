package handler

import (
	"net/http"
	"testing"

	"github.com/Pacerino/CaddyProxyManager/internal/database"
)

func TestCreateUserHashesSecret(t *testing.T) {
	h := newTestHandler(t)
	body := map[string]any{
		"Name":   "Alice",
		"Email":  "alice@example.com",
		"secret": "password123",
	}
	rec := doRequest(h.CreateUser(), http.MethodPost, "/users", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}

	var user database.User
	if err := h.DB.Where("email = ?", "alice@example.com").First(&user).Error; err != nil {
		t.Fatal(err)
	}
	if user.Secret == "" || user.Secret == "password123" {
		t.Error("secret should be stored as a hash")
	}
}

func TestCreateUserValidationFails(t *testing.T) {
	h := newTestHandler(t)
	// Missing email -> validation error.
	body := map[string]any{"Name": "NoEmail", "secret": "x"}
	rec := doRequest(h.CreateUser(), http.MethodPost, "/users", body, nil)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}

func TestGetUsersAndGetUser(t *testing.T) {
	h := newTestHandler(t)
	u := database.User{Name: "Bob", Email: "bob@example.com", Secret: "h"}
	if err := h.DB.Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	list := doRequest(h.GetUsers(), http.MethodGet, "/users", nil, nil)
	var users []database.User
	decodeResult(t, list, &users)
	if len(users) != 1 || users[0].Email != "bob@example.com" {
		t.Fatalf("users = %+v", users)
	}

	get := doRequest(h.GetUser(), http.MethodGet, "/users/1", nil, map[string]string{"userID": "1"})
	var got database.User
	decodeResult(t, get, &got)
	if got.ID != u.ID {
		t.Fatalf("got user %+v", got)
	}
}

func TestGetUserNotFound(t *testing.T) {
	h := newTestHandler(t)
	rec := doRequest(h.GetUser(), http.MethodGet, "/users/99", nil, map[string]string{"userID": "99"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing user, got %d", rec.Code)
	}
}

func TestUpdateUser(t *testing.T) {
	h := newTestHandler(t)
	u := database.User{Name: "Carol", Email: "carol@example.com", Secret: "h"}
	if err := h.DB.Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	body := map[string]any{"ID": u.ID, "Name": "Caroline", "Email": "carol@example.com"}
	rec := doRequest(h.UpdateUser(), http.MethodPut, "/users", body, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}

	var got database.User
	h.DB.First(&got, u.ID)
	if got.Name != "Caroline" {
		t.Errorf("name not updated: %q", got.Name)
	}
}

func TestDeleteUser(t *testing.T) {
	h := newTestHandler(t)
	u := database.User{Name: "Dave", Email: "dave@example.com", Secret: "h"}
	if err := h.DB.Create(&u).Error; err != nil {
		t.Fatal(err)
	}

	rec := doRequest(h.DeleteUser(), http.MethodDelete, "/users/1", nil, map[string]string{"userID": "1"})
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d (%s)", rec.Code, rec.Body.String())
	}

	var count int64
	h.DB.Unscoped().Model(&database.User{}).Count(&count)
	if count != 0 {
		t.Fatalf("expected user removed, count=%d", count)
	}
}

func TestDeleteUserNotFound(t *testing.T) {
	h := newTestHandler(t)
	rec := doRequest(h.DeleteUser(), http.MethodDelete, "/users/99", nil, map[string]string{"userID": "99"})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d", rec.Code)
	}
}
