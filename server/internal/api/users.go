package api

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/roxy-media/roxy/server/internal/auth"
	"github.com/roxy-media/roxy/server/internal/model"
)

// Users (admin): Jellyfin's Dashboard > Users with three levels. Every account has its
// own password and its own profiles. The server always keeps at least one admin who can
// sign in, and an admin can't lock themselves out.

type userView struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Role       string `json:"role"`
	IsDisabled bool   `json:"isDisabled"`
	IsYou      bool   `json:"isYou"`
	CreatedAt  string `json:"createdAt"`
}

func viewUser(u *model.User, me string) userView {
	return userView{ID: u.ID, Name: u.Name, Role: u.Role, IsDisabled: u.IsDisabled, IsYou: u.ID == me, CreatedAt: u.CreatedAt.UTC().Format("2006-01-02T15:04:05Z")}
}

// GET /v1/users -> {users}
func (s *Server) listUsers(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	p := auth.PrincipalFrom(r.Context())
	list, err := s.store.ListUsers(r.Context())
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	out := make([]userView, 0, len(list))
	for i := range list {
		out = append(out, viewUser(&list[i], p.UserID))
	}
	writeJSON(w, 200, map[string]any{"users": out})
}

type userBody struct {
	Name     *string `json:"name"`
	Password *string `json:"password"`
	Role     *string `json:"role"`
	Disabled *bool   `json:"disabled"`
}

// POST /v1/users {name, password, role} -> user
func (s *Server) createUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	p := auth.PrincipalFrom(r.Context())
	var b userBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil || b.Name == nil || b.Password == nil {
		writeError(w, 400, "name and password are required")
		return
	}
	name := strings.TrimSpace(*b.Name)
	if err := s.checkUserName(r, name, ""); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	if err := auth.ValidateNewPassword(*b.Password); err != nil {
		writeError(w, 400, err.Error())
		return
	}
	role := model.RoleRequester
	if b.Role != nil {
		if !model.ValidRole(*b.Role) {
			writeError(w, 400, "role must be admin, requester or basic")
			return
		}
		role = *b.Role
	}
	hash, err := auth.HashPassword(*b.Password)
	if err != nil {
		writeError(w, 500, err.Error())
		return
	}
	u := &model.User{ID: uuid.NewString(), Name: name, PasswordHash: hash, Role: role}
	if err := s.store.CreateUser(r.Context(), u); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	slog.Info("user created", "name", u.Name, "role", u.Role, "by", p.User.Name)
	writeJSON(w, 201, viewUser(u, p.UserID))
}

// PATCH /v1/users/{id} {name?, password?, role?, disabled?} -> user
// A new password or disabling signs the user out everywhere.
func (s *Server) updateUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	p := auth.PrincipalFrom(r.Context())
	u, err := s.store.GetUser(r.Context(), chi.URLParam(r, "id"))
	if err != nil || u == nil {
		writeError(w, 404, "user not found")
		return
	}
	var b userBody
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096)).Decode(&b); err != nil {
		writeError(w, 400, "invalid request body")
		return
	}
	self := u.ID == p.UserID
	wasActiveAdmin := u.Role == model.RoleAdmin && !u.IsDisabled
	if b.Name != nil {
		name := strings.TrimSpace(*b.Name)
		if err := s.checkUserName(r, name, u.ID); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		u.Name = name
	}
	if b.Role != nil {
		if !model.ValidRole(*b.Role) {
			writeError(w, 400, "role must be admin, requester or basic")
			return
		}
		if self && *b.Role != model.RoleAdmin {
			writeError(w, 400, "you can't take admin away from yourself; another admin can")
			return
		}
		u.Role = *b.Role
	}
	signOut := false
	if b.Disabled != nil {
		if self && *b.Disabled {
			writeError(w, 400, "you can't disable your own account")
			return
		}
		signOut = *b.Disabled && !u.IsDisabled
		u.IsDisabled = *b.Disabled
	}
	if wasActiveAdmin && (u.Role != model.RoleAdmin || u.IsDisabled) {
		if n, err := s.store.CountActiveAdmins(r.Context()); err == nil && n <= 1 {
			writeError(w, 400, "this is the only admin; make someone else admin first")
			return
		}
	}
	if b.Password != nil {
		if err := auth.ValidateNewPassword(*b.Password); err != nil {
			writeError(w, 400, err.Error())
			return
		}
		hash, err := auth.HashPassword(*b.Password)
		if err != nil {
			writeError(w, 500, err.Error())
			return
		}
		if err := s.store.SetPasswordHash(r.Context(), u.ID, hash); err != nil {
			writeError(w, 500, err.Error())
			return
		}
		signOut = !self // an admin resetting their own keeps this device (use Change password)
	}
	if err := s.store.UpdateUser(r.Context(), u); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	if signOut {
		_ = s.store.DeleteDevicesExcept(r.Context(), u.ID, "")
	}
	slog.Info("user updated", "name", u.Name, "role", u.Role, "disabled", u.IsDisabled, "password_reset", b.Password != nil, "by", p.User.Name)
	writeJSON(w, 200, viewUser(u, p.UserID))
}

// DELETE /v1/users/{id} -> 204. Their profiles, watch history and devices go with them;
// their requests stay (under their name).
func (s *Server) deleteUser(w http.ResponseWriter, r *http.Request) {
	if !s.requireAdmin(w, r) {
		return
	}
	p := auth.PrincipalFrom(r.Context())
	u, err := s.store.GetUser(r.Context(), chi.URLParam(r, "id"))
	if err != nil || u == nil {
		writeError(w, 404, "user not found")
		return
	}
	if u.ID == p.UserID {
		writeError(w, 400, "you can't delete your own account")
		return
	}
	if u.Role == model.RoleAdmin && !u.IsDisabled {
		if n, err := s.store.CountActiveAdmins(r.Context()); err == nil && n <= 1 {
			writeError(w, 400, "this is the only admin")
			return
		}
	}
	if err := s.store.DeleteUser(r.Context(), u.ID, p.UserID); err != nil {
		writeError(w, 500, err.Error())
		return
	}
	slog.Info("user deleted", "name", u.Name, "by", p.User.Name)
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) checkUserName(r *http.Request, name, selfID string) error {
	if name == "" || len(name) > 40 {
		return errors.New("a name of 1 to 40 characters is required")
	}
	if strings.ContainsAny(name, "/\\\"<>") {
		return errors.New("names can't contain / \\ \" < or >")
	}
	if other, err := s.store.GetUserByName(r.Context(), name); err == nil && other != nil && other.ID != selfID {
		return errors.New("someone already has that name")
	}
	return nil
}
