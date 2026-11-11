// Package controlplane implements the versioned private administration API.
// Every lookup is synchronous; errors never fall back to stale local authority.
package controlplane

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"schakker_shared/authority"
	"strconv"
	"strings"
	"time"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/database/dao"
)

var ErrUnavailable = errors.New("control-plane authority is unavailable")

type Client struct {
	base, token string
	http        *http.Client
}

func New(rawURL, token string, production bool) (*Client, error) {
	u, err := url.Parse(rawURL)
	if err != nil || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") || len(token) < 32 {
		return nil, errors.New("configure SCHAKKER_CONTROL_PLANE_URL and a service token of at least 32 bytes")
	}
	if u.Scheme != "https" {
		ip := net.ParseIP(u.Hostname())
		if u.Scheme != "http" || production || ip == nil || !ip.IsLoopback() {
			return nil, errors.New("control-plane API requires verified HTTPS; HTTP loopback is allowed only in development")
		}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = &tls.Config{MinVersion: tls.VersionTLS12}
	return &Client{base: strings.TrimRight(u.String(), "/"), token: token, http: &http.Client{Transport: transport, Timeout: 5 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}, nil
}
func (c *Client) request(ctx context.Context, method, path string, input, output any) error {
	var body io.Reader
	if input != nil {
		raw, err := json.Marshal(input)
		if err != nil {
			return ErrUnavailable
		}
		body = bytes.NewReader(raw)
	}
	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return ErrUnavailable
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	response, err := c.http.Do(req)
	if err != nil {
		return ErrUnavailable
	}
	defer response.Body.Close()
	switch response.StatusCode {
	case 200:
		if json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(output) != nil {
			return ErrUnavailable
		}
		return nil
	case 401:
		// Only the credential-verification operation uses 401 for a user failure.
		if path == "/internal/v1/authenticate-user" {
			return access.ErrUnauthenticated
		}
		return ErrUnavailable
	case 403:
		return access.ErrForbidden
	case 404:
		return access.ErrNotFound
	default:
		return ErrUnavailable
	}
}

type userDTO struct {
	ID                    int64
	Username, DisplayName string
	Active                bool
	Version               int64
	Rotation              int64
}

func (u userDTO) user() (dao.User, error) {
	if u.ID < 1 || u.Version < 1 || u.Username == "" {
		return dao.User{}, ErrUnavailable
	}
	return dao.User{ID: u.ID, Username: u.Username, DisplayName: u.DisplayName, Active: u.Active, Version: u.Version, Rotation: u.Rotation}, nil
}
func (c *Client) AuthenticateUser(ctx context.Context, username, password string) (dao.User, error) {
	var result userDTO
	err := c.request(ctx, http.MethodPost, "/internal/v1/authenticate-user", struct{ Username, Password string }{username, password}, &result)
	if err != nil {
		return dao.User{}, err
	}
	return result.user()
}
func (c *Client) User(ctx context.Context, id int64) (dao.User, error) {
	var result userDTO
	err := c.request(ctx, "GET", fmt.Sprintf("/internal/v1/users/%d", id), nil, &result)
	if err != nil {
		return dao.User{}, err
	}
	return result.user()
}
func (c *Client) EffectivePermission(ctx context.Context, user, database int64) (dao.Permission, error) {
	var out struct{ Permission dao.Permission }
	err := c.request(ctx, "GET", fmt.Sprintf("/internal/v1/users/%d/databases/%d/permission", user, database), nil, &out)
	if errors.Is(err, access.ErrNotFound) || errors.Is(err, access.ErrForbidden) {
		return dao.NoAccess, nil
	}
	if err != nil {
		return dao.NoAccess, err
	}
	if out.Permission != dao.NoAccess && out.Permission != dao.ViewOnly && out.Permission != dao.ViewEdit {
		return dao.NoAccess, ErrUnavailable
	}
	return out.Permission, nil
}
func (c *Client) AccessibleDatabases(ctx context.Context, user int64) ([]dao.Database, error) {
	var list []dao.Database
	err := c.request(ctx, "GET", fmt.Sprintf("/internal/v1/users/%d/databases", user), nil, &list)
	if errors.Is(err, access.ErrNotFound) || errors.Is(err, access.ErrForbidden) {
		return []dao.Database{}, nil
	}
	return list, err
}
func (c *Client) Connection(ctx context.Context, user, database int64, required dao.Permission) (dao.DatabaseMetadata, error) {
	var out struct {
		dao.Database
		Username, Password string
	}
	err := c.request(ctx, "GET", "/internal/v1/users/"+strconv.FormatInt(user, 10)+"/databases/"+strconv.FormatInt(database, 10)+"/connection?required="+url.QueryEscape(string(required)), nil, &out)
	if err != nil {
		return dao.DatabaseMetadata{}, err
	}
	if out.ID != database || !out.Active || out.Username == "" {
		return dao.DatabaseMetadata{}, ErrUnavailable
	}
	return dao.DatabaseMetadata{Database: out.Database, Username: out.Username, Password: out.Password}, nil
}

var _ access.Authority = (*Client)(nil)

func (c *Client) MigrationAuthority(ctx context.Context, actor, database int64) (authority.Snapshot, error) {
	var out authority.Snapshot
	err := c.request(ctx, "GET", fmt.Sprintf("/internal/v1/users/%d/databases/%d/migration-authority", actor, database), nil, &out)
	if err != nil {
		return out, err
	}
	if out.DatabaseID != database || out.Revision < 1 || len(out.ConnectionFingerprint) != 64 {
		return authority.Snapshot{}, ErrUnavailable
	}
	seen := map[int64]bool{}
	for _, g := range out.Groups {
		if g.ID < 1 || seen[g.ID] || (g.Permission != "view_only" && g.Permission != "view_edit") {
			return authority.Snapshot{}, ErrUnavailable
		}
		seen[g.ID] = true
	}
	return out, nil
}
