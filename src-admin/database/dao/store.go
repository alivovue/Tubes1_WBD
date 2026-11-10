// Package dao fixes the shared authentication DAO to the admin-only schema.
package dao

import (
	"database/sql"
	shared "tubes1_wbd/database/dao"
)

func New(db *sql.DB) (*shared.Store, error) { return shared.NewAdminAuthentication(db) }
