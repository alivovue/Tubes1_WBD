package dao

import "time"

type User struct {
	ID          int64
	Username    string
	DisplayName string
	IsAdmin     bool
	Active      bool
}

type Credentials struct {
	User
	PasswordHash string `json:"-"`
}

type Group struct {
	ID          int64
	Name        string
	Description string
}

type Membership struct {
	UserID  int64
	GroupID int64
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

type Grant struct {
	GroupID    int64
	DatabaseID int64
	Permission Permission
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
	Username          string
	EncryptedPassword []byte `json:"-"`
}

type AdminData struct {
	Users       []User
	Groups      []Group
	Memberships []Membership
	Databases   []Database
	Grants      []Grant
}

type Session struct {
	ID              string
	UserID          int64
	IdleExpires     time.Time
	AbsoluteExpires time.Time
	Revoked         bool
}

type SessionToken struct {
	IssuedAt       time.Time
	ReplacedAt     *time.Time
	GraceExpiresAt *time.Time
}
