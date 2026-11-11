// Package internalapi is the private control-plane contract. It is mounted on a
// separate listener and is never registered on the browser-facing application.
package internalapi

import (
	"crypto/subtle"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"strconv"
	"strings"

	"schakker_shared/auth"

	"tubes1_wbd_admin/backend/access"
	"tubes1_wbd_admin/backend/connectionconfig"
	"tubes1_wbd_admin/database/dao"
)

type server struct {
	store *dao.Store
	key   []byte
	token string
}

func NewHandler(store *dao.Store, key []byte, token string) (http.Handler, error) {
	if store == nil || len(key) != 32 || len(token) < 32 {
		return nil, errors.New("internal API requires a metadata store, encryption key, and a service token of at least 32 bytes")
	}
	return &server{store: store, key: append([]byte(nil), key...), token: token}, nil
}

type userResponse struct {
	ID                    int64
	Username, DisplayName string
	Active                bool
	Version               int64
	Rotation              int64
}

func userDTO(u dao.User) userResponse {
	return userResponse{u.ID, u.Username, u.DisplayName, u.Active, u.Version, u.Rotation}
}
func send(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(value)
}
func (s *server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	w.Header().Set("X-Content-Type-Options", "nosniff")
	host, _, _ := net.SplitHostPort(r.RemoteAddr)
	ip := net.ParseIP(host)
	// Cross-host calls require TLS. Browser contexts and cookies never confer
	// service authority, even if this listener is accidentally reverse-proxied.
	if (r.TLS == nil && (ip == nil || !ip.IsLoopback())) || r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Mode") != "" || r.Header.Get("Cookie") != "" || subtle.ConstantTimeCompare([]byte(r.Header.Get("Authorization")), []byte("Bearer "+s.token)) != 1 {
		send(w, 401, map[string]string{"error": "service authentication required"})
		return
	}
	if r.Method == http.MethodPost && r.URL.Path == "/internal/v1/authenticate-user" {
		var in struct{ Username, Password string }
		r.Body = http.MaxBytesReader(w, r.Body, 16<<10)
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()
		if dec.Decode(&in) != nil || len(in.Username) > 100 || len(in.Password) > 72 {
			send(w, 400, map[string]string{"error": "invalid credentials"})
			return
		}
		c, err := s.store.Credentials(r.Context(), in.Username)
		if err != nil && !errors.Is(err, dao.ErrNotFound) {
			send(w, 503, map[string]string{"error": "authority unavailable"})
			return
		}
		// Always run an equal-cost password comparison, including missing users.
		hash := c.PasswordHash
		if err != nil {
			hash = access.DummyPasswordHash()
		}
		valid := auth.VerifyPassword(hash, in.Password)
		if !valid || err != nil || !c.Active {
			send(w, 401, map[string]string{"error": "invalid credentials"})
			return
		}
		// Re-read after bcrypt so a concurrent password/deactivation change cannot
		// authenticate credentials that were superseded during the comparison.
		current, err := s.store.Credentials(r.Context(), in.Username)
		if err != nil || !current.Active || current.PasswordHash != c.PasswordHash || current.Version != c.Version {
			send(w, 401, map[string]string{"error": "invalid credentials"})
			return
		}
		send(w, 200, userDTO(current.User))
		return
	}
	if r.Method != http.MethodGet {
		send(w, 405, map[string]string{"error": "method not allowed"})
		return
	}
	parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
	if len(parts) < 4 || parts[0] != "internal" || parts[1] != "v1" || parts[2] != "users" {
		http.NotFound(w, r)
		return
	}
	uid, err := strconv.ParseInt(parts[3], 10, 64)
	if err != nil || uid < 1 {
		send(w, 400, map[string]string{"error": "invalid identity"})
		return
	}
	u, err := s.store.User(r.Context(), uid)
	if errors.Is(err, dao.ErrNotFound) {
		send(w, 404, map[string]string{"error": "user not found"})
		return
	}
	if err != nil {
		send(w, 503, map[string]string{"error": "authority unavailable"})
		return
	}
	if !u.Active {
		send(w, 403, map[string]string{"error": "user disabled"})
		return
	}
	if len(parts) == 4 {
		send(w, 200, userDTO(u))
		return
	}
	if parts[4] != "databases" {
		http.NotFound(w, r)
		return
	}
	if len(parts) == 5 {
		list, err := s.store.AccessibleDatabases(r.Context(), uid)
		if err != nil {
			send(w, 503, map[string]string{"error": "authority unavailable"})
			return
		}
		send(w, 200, list)
		return
	}
	if len(parts) != 7 {
		http.NotFound(w, r)
		return
	}
	did, err := strconv.ParseInt(parts[5], 10, 64)
	if err != nil || did < 1 {
		send(w, 400, map[string]string{"error": "invalid database"})
		return
	}
	if parts[6] == "migration-authority" {
		snapshot, err := s.store.MigrationAuthority(r.Context(), uid, did)
		if errors.Is(err, dao.ErrNotFound) {
			send(w, 403, map[string]string{"error": "permission denied"})
			return
		}
		if err != nil {
			send(w, 503, map[string]string{"error": "authority unavailable"})
			return
		}
		send(w, 200, snapshot)
		return
	}
	permission, err := s.store.EffectivePermission(r.Context(), uid, did)
	if err != nil {
		send(w, 503, map[string]string{"error": "authority unavailable"})
		return
	}
	if parts[6] == "permission" {
		send(w, 200, struct{ Permission dao.Permission }{permission})
		return
	}
	if parts[6] != "connection" {
		http.NotFound(w, r)
		return
	}
	required := dao.Permission(r.URL.Query().Get("required"))
	if (required != dao.ViewOnly && required != dao.ViewEdit) || !permission.Allows(required) {
		send(w, 403, map[string]string{"error": "permission denied"})
		return
	}
	m, err := s.store.DatabaseMetadata(r.Context(), did)
	if err != nil {
		send(w, 503, map[string]string{"error": "authority unavailable"})
		return
	}
	if !m.Active {
		send(w, 403, map[string]string{"error": "database disabled"})
		return
	}
	password, err := connectionconfig.DecryptPassword(s.key, m.EncryptedPassword)
	if err != nil {
		send(w, 503, map[string]string{"error": "connection unavailable"})
		return
	}
	send(w, 200, struct {
		dao.Database
		Username, Password string
	}{m.Database, m.Username, password})
}
