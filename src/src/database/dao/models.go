package dao

import "time"

type User struct {
	ID          int64
	Username    string
	DisplayName string
	Active      bool
	Version     int64 `json:"-"`
	Rotation    int64 `json:"-"`
}

type Permission string

const (
	NoAccess Permission = ""
	ViewOnly Permission = "view_only"
	ViewEdit Permission = "view_edit"
)

func (p Permission) Allows(required Permission) bool {
	return (required == ViewOnly && (p == ViewOnly || p == ViewEdit)) || (required == ViewEdit && p == ViewEdit)
}

type Database struct {
	DBMS         string
	ID           int64
	Name         string
	Host         string
	DatabaseName string
	SSLMode      string
	Port         int
	Active       bool
	Permission   Permission
}

type DatabaseMetadata struct {
	Database
	Username string
	Password string `json:"-"`
}

type Session struct {
	ID              string
	UserID          int64
	IdleExpires     time.Time
	AbsoluteExpires time.Time
	Revoked         bool
	Version         int64
	Rotation        int64
}

type SessionToken struct {
	IssuedAt       time.Time
	ReplacedAt     *time.Time
	GraceExpiresAt *time.Time
}
