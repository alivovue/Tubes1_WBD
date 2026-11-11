// Package authority defines the secret-free contract shared by the two backends.
package authority

import "context"

type Group struct {
	ID         int64  `json:"group_id"`
	Name       string `json:"name"`
	LeaderID   int64  `json:"leader_user_id"`
	LeaderName string `json:"leader_name"`
	Permission string `json:"access_level"`
	Valid      bool   `json:"leader_valid"`
}

type Snapshot struct {
	DatabaseID            int64   `json:"database_id"`
	Revision              int64   `json:"authority_revision"`
	ConnectionFingerprint string  `json:"connection_fingerprint"`
	Groups                []Group `json:"groups"`
}

type Reviewer interface {
	MigrationAuthority(context.Context, int64, int64) (Snapshot, error)
}
