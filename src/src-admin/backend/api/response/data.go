package response

import (
	"time"

	"tubes1_wbd_admin/database/dao"
)

type GroupView struct {
	dao.Group
	Members []dao.User
	Grants  []GrantCell
	Editor  string
	Focus   bool
}
type GrantCell struct {
	Database   dao.Database
	Permission dao.Permission
}
type Data struct {
	Title, Active, Section string
	User                   dao.User
	CSRF, Error, Message   string
	Now, Expires           time.Time
	Administrators         []dao.User
	Admin                  dao.AdminData
	Groups                 []GroupView
	DBMS                   []string
}
