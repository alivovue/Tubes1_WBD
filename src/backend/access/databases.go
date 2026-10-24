package access

import (
	"context"
	"database/sql"
	"errors"
	"net"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type ConnectionInput struct {
	Name         string `json:"name"`
	Host         string `json:"host"`
	DatabaseName string `json:"database_name"`
	Username     string `json:"db_username"`
	Password     string `json:"password"`
	SSLMode      string `json:"sslmode"`
	Port         int    `json:"port"`
	Active       bool   `json:"is_active"`
}

func (in ConnectionInput) validate(secure bool) error {
	if !cleanName(in.Name, 150) || !cleanName(in.Host, 255) || !cleanName(in.DatabaseName, 100) || !cleanName(in.Username, 100) || len(in.Password) > 4096 || in.Port < 1 || in.Port > 65535 {
		return ErrInvalid
	}
	if strings.ContainsAny(in.Host, "/\\\x00 \r\n\t") {
		return ErrInvalid
	}
	if in.SSLMode != "disable" && in.SSLMode != "require" && in.SSLMode != "verify-full" {
		return ErrInvalid
	}
	if secure && in.SSLMode != "verify-full" {
		return errors.New("produksi membutuhkan SSL mode verify-full")
	}
	return nil
}

func connectionDSN(in ConnectionInput) string {
	u := url.URL{Scheme: "postgresql", User: url.UserPassword(in.Username, in.Password), Host: net.JoinHostPort(in.Host, strconv.Itoa(in.Port)), Path: "/" + in.DatabaseName}
	q := url.Values{"sslmode": {in.SSLMode}, "connect_timeout": {"5"}}
	u.RawQuery = q.Encode()
	return u.String()
}

func testConnection(ctx context.Context, in ConnectionInput) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	db, err := OpenDatabase(ctx, connectionDSN(in))
	if err != nil {
		return errors.New("koneksi target gagal; periksa host, port, database, kredensial, dan TLS")
	}
	return db.Close()
}

// SaveConnection tests a PostgreSQL target before storing an AES-GCM encrypted password.
// On update a blank password preserves the stored credential; the browser never receives it.
func (s *Service) SaveConnection(ctx context.Context, actor, id int64, in ConnectionInput) error {
	if err := in.validate(s.config.SecureCookies); err != nil {
		return ErrInvalid
	}
	return s.adminChange(ctx, actor, func(tx *sql.Tx) error {
		if id != 0 && in.Password == "" {
			var encrypted []byte
			if err := tx.QueryRowContext(ctx, `SELECT encrypted_password FROM managed_databases WHERE id=$1`, id).Scan(&encrypted); err != nil {
				return err
			}
			var err error
			in.Password, err = decryptSecret(s.config.Key, encrypted)
			if err != nil {
				return errors.New("kredensial target tidak dapat dibuka; periksa master key")
			}
		}
		if err := testConnection(ctx, in); err != nil {
			return err
		}
		encrypted, err := encryptSecret(s.config.Key, in.Password)
		if err != nil {
			return err
		}
		options := `{"sslmode":"` + in.SSLMode + `"}`
		if id == 0 {
			_, err = tx.ExecContext(ctx, `INSERT INTO managed_databases(display_name,host,port,database_name,db_username,encrypted_password,connection_options,is_active,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9)`, in.Name, in.Host, in.Port, in.DatabaseName, in.Username, encrypted, options, in.Active, actor)
			return err
		}
		if err = affected(tx.ExecContext(ctx, `UPDATE managed_databases SET display_name=$2,host=$3,port=$4,database_name=$5,db_username=$6,encrypted_password=$7,connection_options=$8,is_active=$9,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, in.Name, in.Host, in.Port, in.DatabaseName, in.Username, encrypted, options, in.Active)); err != nil {
			return err
		}
		return s.revokeUsers(ctx, tx, `SELECT m.user_id FROM user_groups m JOIN group_database_permissions p ON p.group_id=m.group_id WHERE p.database_id=$1`, id)
	})
}

// DisableConnection is a soft removal: existing migration history retains its FK target.
func (s *Service) DisableConnection(ctx context.Context, actor, id int64) error {
	return s.adminChange(ctx, actor, func(tx *sql.Tx) error {
		if err := affected(tx.ExecContext(ctx, `UPDATE managed_databases SET is_active=FALSE,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id)); err != nil {
			return err
		}
		return s.revokeUsers(ctx, tx, `SELECT m.user_id FROM user_groups m JOIN group_database_permissions p ON p.group_id=m.group_id WHERE p.database_id=$1`, id)
	})
}

// OpenTarget is the integration point for explorer/migration services. It always checks
// current permissions before decrypting a credential. Call per operation, then Close.
func (s *Service) OpenTarget(ctx context.Context, user, database int64, required Permission) (*sql.DB, error) {
	if err := s.RequirePermission(ctx, user, database, required); err != nil {
		return nil, err
	}
	var in ConnectionInput
	var encrypted []byte
	err := s.db.QueryRowContext(ctx, `SELECT host,port,database_name,db_username,encrypted_password,COALESCE(connection_options->>'sslmode','require') FROM managed_databases WHERE id=$1 AND is_active AND dbms_type='postgresql'`, database).Scan(&in.Host, &in.Port, &in.DatabaseName, &in.Username, &encrypted, &in.SSLMode)
	if err != nil {
		return nil, classify(err)
	}
	in.Password, err = decryptSecret(s.config.Key, encrypted)
	if err != nil {
		return nil, errors.New("kredensial target tidak tersedia")
	}
	return OpenDatabase(ctx, connectionDSN(in))
}

func (s *Service) AccessibleDatabases(ctx context.Context, user int64) ([]Database, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT d.id,d.display_name,CASE MAX(CASE p.access_level WHEN 'view_edit' THEN 2 ELSE 1 END) WHEN 2 THEN 'view_edit' ELSE 'view_only' END
	 FROM managed_databases d JOIN group_database_permissions p ON p.database_id=d.id JOIN user_groups m ON m.group_id=p.group_id JOIN users u ON u.id=m.user_id
	 WHERE u.id=$1 AND u.is_active AND d.is_active GROUP BY d.id,d.display_name ORDER BY d.display_name`, user)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	result := make([]Database, 0)
	for rows.Next() {
		var d Database
		if err := rows.Scan(&d.ID, &d.Name, &d.Permission); err != nil {
			return nil, err
		}
		result = append(result, d)
	}
	return result, rows.Err()
}

type AdminData struct {
	Users       []User       `json:"users"`
	Groups      []Group      `json:"groups"`
	Memberships []Membership `json:"memberships"`
	Databases   []Database   `json:"databases"`
	Grants      []Grant      `json:"grants"`
}

func (s *Service) Administration(ctx context.Context, actor int64) (AdminData, error) {
	result := AdminData{Users: []User{}, Groups: []Group{}, Memberships: []Membership{}, Databases: []Database{}, Grants: []Grant{}}
	var allowed bool
	if err := s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users WHERE id=$1 AND is_active AND is_platform_admin)`, actor).Scan(&allowed); err != nil {
		return result, err
	}
	if !allowed {
		return result, ErrForbidden
	}
	rows, err := s.db.QueryContext(ctx, `SELECT `+userColumns+` FROM users ORDER BY id`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		u, e := scanUser(rows)
		if e != nil {
			rows.Close()
			return result, e
		}
		result.Users = append(result.Users, u)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT id,name,COALESCE(description,'') FROM access_groups ORDER BY name`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var g Group
		if err = rows.Scan(&g.ID, &g.Name, &g.Description); err != nil {
			rows.Close()
			return result, err
		}
		result.Groups = append(result.Groups, g)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT user_id,group_id FROM user_groups ORDER BY user_id,group_id`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var m Membership
		if err = rows.Scan(&m.UserID, &m.GroupID); err != nil {
			rows.Close()
			return result, err
		}
		result.Memberships = append(result.Memberships, m)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT id,display_name,host,port,database_name,is_active,COALESCE(connection_options->>'sslmode','require') FROM managed_databases ORDER BY id`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var d Database
		if err = rows.Scan(&d.ID, &d.Name, &d.Host, &d.Port, &d.DatabaseName, &d.Active, &d.SSLMode); err != nil {
			rows.Close()
			return result, err
		}
		result.Databases = append(result.Databases, d)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return result, err
	}
	rows, err = s.db.QueryContext(ctx, `SELECT group_id,database_id,access_level FROM group_database_permissions ORDER BY group_id,database_id`)
	if err != nil {
		return result, err
	}
	for rows.Next() {
		var g Grant
		if err = rows.Scan(&g.GroupID, &g.DatabaseID, &g.Permission); err != nil {
			rows.Close()
			return result, err
		}
		result.Grants = append(result.Grants, g)
	}
	err = rows.Err()
	rows.Close()
	return result, err
}
