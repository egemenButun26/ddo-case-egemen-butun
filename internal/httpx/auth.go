package httpx

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"parkakil/internal/auth"
	"parkakil/internal/store"
)

const maxAuthBodyBytes = 8 << 10

type loginRequest struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type loginUser struct {
	ID    int64     `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
	Role  auth.Role `json:"role"`
}

type loginResponse struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
	User      loginUser `json:"user"`
}

type userView struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      auth.Role `json:"role"`
	Plate     *string   `json:"plate"`
	Active    bool      `json:"active"`
	CreatedAt time.Time `json:"created_at"`
}

type adminUsersResponse struct {
	Items       []userView `json:"items"`
	NextAfterID *int64     `json:"next_after_id"`
}

type authInfo struct {
	User      store.User
	SessionID int64
}

type authInfoKey struct{}

func UserFromContext(ctx context.Context) (store.User, bool) {
	info, ok := ctx.Value(authInfoKey{}).(authInfo)
	if !ok {
		return store.User{}, false
	}
	return info.User, true
}

func requireAuth(deps Deps) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			parts := strings.Fields(r.Header.Get("Authorization"))
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || parts[1] == "" {
				WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
				return
			}
			if deps.DB == nil {
				WriteError(w, http.StatusInternalServerError, "internal", "internal error")
				return
			}
			session, err := store.GetAuthSessionByTokenHash(r.Context(), deps.DB, auth.HashToken(parts[1]))
			if err != nil {
				if !errors.Is(err, store.ErrNotFound) {
					deps.Logger.Error("oturum okunamadi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
				}
				WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
				return
			}
			user, err := store.GetUser(r.Context(), deps.DB, session.UserID)
			if err != nil || !user.Active {
				if err != nil && !errors.Is(err, store.ErrNotFound) {
					deps.Logger.Error("kullanici okunamadi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
				}
				WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
				return
			}
			ctx := context.WithValue(r.Context(), authInfoKey{}, authInfo{User: user, SessionID: session.ID})
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func requireRole(roles ...auth.Role) func(http.Handler) http.Handler {
	allowed := make(map[auth.Role]bool, len(roles))
	for _, role := range roles {
		allowed[role] = true
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			info, ok := r.Context().Value(authInfoKey{}).(authInfo)
			if !ok {
				WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
				return
			}
			if !allowed[info.User.Role] {
				WriteError(w, http.StatusForbidden, "forbidden", "forbidden")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

func loginHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if deps.DB == nil {
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxAuthBodyBytes)
		var req loginRequest
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if err := dec.Decode(&req); err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_json", "invalid json")
			return
		}

		email := strings.ToLower(strings.TrimSpace(req.Email))
		user, err := store.GetUserByEmail(r.Context(), deps.DB, email)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			deps.Logger.Error("kullanici okunamadi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}

		found := err == nil
		if !found || !user.Active || !auth.CheckPassword(user.PasswordHash, req.Password) {
			var actorID *int64
			if found {
				id := user.ID
				actorID = &id
			}
			loginFailedAudit(deps, r, email, actorID)
			WriteError(w, http.StatusUnauthorized, "invalid_credentials", "invalid credentials")
			return
		}

		token, tokenHash, err := auth.GenerateToken()
		if err != nil {
			deps.Logger.Error("token uretilemedi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		expiresAt := time.Now().UTC().Add(deps.SessionTTL)
		if _, err := store.CreateAuthSession(r.Context(), deps.DB, user.ID, tokenHash, expiresAt); err != nil {
			deps.Logger.Error("oturum olusturulamadi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}

		if err := store.AppendAudit(r.Context(), deps.DB, store.AuditEntry{
			ActorUserID: &user.ID,
			ActorKind:   store.ActorUser,
			Action:      "auth.login",
			Entity:      "user",
			EntityID:    strconv.FormatInt(user.ID, 10),
			NewValue:    map[string]any{"email": user.Email, "role": string(user.Role)},
			RequestID:   r.Header.Get("X-Request-ID"),
		}); err != nil {
			deps.Logger.Error("denetim kaydi yazilamadi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}

		WriteJSON(w, http.StatusOK, loginResponse{
			Token:     token,
			ExpiresAt: expiresAt,
			User: loginUser{
				ID:    user.ID,
				Email: user.Email,
				Name:  user.Name,
				Role:  user.Role,
			},
		})
	}
}

func loginFailedAudit(deps Deps, r *http.Request, email string, actorID *int64) {
	entityID := email
	if entityID == "" {
		entityID = "-"
	}
	newValue := map[string]any{}
	if email != "" {
		newValue["email"] = email
	}
	if err := store.AppendAudit(r.Context(), deps.DB, store.AuditEntry{
		ActorUserID: actorID,
		ActorKind:   store.ActorUser,
		Action:      "auth.login_failed",
		Entity:      "user",
		EntityID:    entityID,
		NewValue:    newValue,
		RequestID:   r.Header.Get("X-Request-ID"),
	}); err != nil {
		deps.Logger.Error("denetim kaydi yazilamadi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
	}
}

func logoutHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		info, ok := r.Context().Value(authInfoKey{}).(authInfo)
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		if err := store.RevokeAuthSession(r.Context(), deps.DB, info.SessionID); err != nil {
			deps.Logger.Error("oturum kapatilamadi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		if err := store.AppendAudit(r.Context(), deps.DB, store.AuditEntry{
			ActorUserID: &info.User.ID,
			ActorKind:   store.ActorUser,
			Action:      "auth.logout",
			Entity:      "user",
			EntityID:    strconv.FormatInt(info.User.ID, 10),
			RequestID:   r.Header.Get("X-Request-ID"),
		}); err != nil {
			deps.Logger.Error("denetim kaydi yazilamadi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}
}

func meHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		user, ok := UserFromContext(r.Context())
		if !ok {
			WriteError(w, http.StatusUnauthorized, "unauthorized", "unauthorized")
			return
		}
		WriteJSON(w, http.StatusOK, viewUser(user))
	}
}

func adminUsersHandler(deps Deps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		afterID, err := parseAfterID(r.URL.Query().Get("after_id"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_pagination", "invalid after_id")
			return
		}
		limit, err := parseLimit(r.URL.Query().Get("limit"))
		if err != nil {
			WriteError(w, http.StatusBadRequest, "invalid_pagination", "invalid limit")
			return
		}
		users, err := store.ListUsers(r.Context(), deps.DB, afterID, limit+1)
		if err != nil {
			deps.Logger.Error("kullanici listesi okunamadi", "hata", err, "request_id", r.Header.Get("X-Request-ID"))
			WriteError(w, http.StatusInternalServerError, "internal", "internal error")
			return
		}
		var next *int64
		if len(users) > limit {
			nextID := users[limit-1].ID
			next = &nextID
			users = users[:limit]
		}
		items := make([]userView, 0, len(users))
		for _, user := range users {
			items = append(items, viewUser(user))
		}
		WriteJSON(w, http.StatusOK, adminUsersResponse{Items: items, NextAfterID: next})
	}
}

func parseAfterID(raw string) (int64, error) {
	if raw == "" {
		return 0, nil
	}
	id, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || id < 0 {
		return 0, errors.New("gecersiz after_id")
	}
	return id, nil
}

func parseLimit(raw string) (int, error) {
	if raw == "" {
		return 20, nil
	}
	limit, err := strconv.Atoi(raw)
	if err != nil || limit < 1 || limit > 100 {
		return 0, errors.New("gecersiz limit")
	}
	return limit, nil
}

func viewUser(user store.User) userView {
	return userView{
		ID:        user.ID,
		Email:     user.Email,
		Name:      user.Name,
		Role:      user.Role,
		Plate:     user.Plate,
		Active:    user.Active,
		CreatedAt: user.CreatedAt,
	}
}
