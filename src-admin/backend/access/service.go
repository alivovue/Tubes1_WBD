// Package access binds the shared password, session and CSRF logic to admin authentication.
package access

import (
	"crypto/hmac"
	"crypto/sha256"
	shared "tubes1_wbd/backend/access"
	"tubes1_wbd/database/dao"
)

func NewService(store *dao.Store, config shared.Config) (*shared.Service, error) {
	if store == nil || !store.AdminAuthentication() {
		return nil, shared.ErrInvalid
	}
	mac := hmac.New(sha256.New, config.Key)
	mac.Write([]byte("schakker/admin-auth/v1"))
	config.Key = mac.Sum(nil)
	return shared.NewService(store, config)
}
