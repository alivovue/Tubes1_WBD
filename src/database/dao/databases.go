package dao

import (
	"context"
	"encoding/json"
	"time"
)

func (d *Store) SaveDatabaseMetadata(ctx context.Context, actor int64, in DatabaseMetadata, now time.Time) (int64, error) {
	if in.DBMS == "" {
		in.DBMS = "postgresql"
	}
	if len(in.DBMS) > 30 {
		return 0, ErrInvalid
	}
	if in.ID < 0 || in.Name == "" || in.Host == "" || in.DatabaseName == "" || in.Username == "" || len(in.EncryptedPassword) == 0 || in.Port < 1 || in.Port > 65535 {
		return 0, ErrInvalid
	}
	if in.SSLMode != "disable" && in.SSLMode != "require" && in.SSLMode != "verify-full" {
		return 0, ErrInvalid
	}
	options, err := json.Marshal(map[string]string{"sslmode": in.SSLMode})
	if err != nil {
		return 0, err
	}
	id := in.ID
	err = d.Transaction(ctx, func(tx *Store) error {
		if id == 0 {
			return tx.q.QueryRowContext(ctx, `INSERT INTO managed_databases(display_name,dbms_type,host,port,database_name,db_username,encrypted_password,connection_options,is_active,created_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10) RETURNING id`, in.Name, in.DBMS, in.Host, in.Port, in.DatabaseName, in.Username, in.EncryptedPassword, string(options), in.Active, actor).Scan(&id)
		}
		if err := affected(tx.q.ExecContext(ctx, `UPDATE managed_databases SET display_name=$2,dbms_type=$3,host=$4,port=$5,database_name=$6,db_username=$7,encrypted_password=$8,connection_options=COALESCE(connection_options,'{}'::jsonb)||$9::jsonb,is_active=$10,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id, in.Name, in.DBMS, in.Host, in.Port, in.DatabaseName, in.Username, in.EncryptedPassword, string(options), in.Active)); err != nil {
			return err
		}
		return tx.RevokeDatabaseSessions(ctx, id, now)
	})
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (d *Store) DatabaseMetadata(ctx context.Context, id int64) (DatabaseMetadata, error) {
	var m DatabaseMetadata
	err := d.q.QueryRowContext(ctx, `SELECT id,display_name,dbms_type,host,port,database_name,is_active,COALESCE(connection_options->>'sslmode','require'),db_username,encrypted_password FROM managed_databases WHERE id=$1`, id).Scan(&m.ID, &m.Name, &m.DBMS, &m.Host, &m.Port, &m.DatabaseName, &m.Active, &m.SSLMode, &m.Username, &m.EncryptedPassword)
	return m, classify(err)
}

func (d *Store) DatabaseNamespace(ctx context.Context, id int64, candidate string) (string, error) {
	var namespace string
	err := d.q.QueryRowContext(ctx, `UPDATE managed_databases SET connection_options=jsonb_set(COALESCE(connection_options,'{}'::jsonb),'{migration_namespace}',to_jsonb(COALESCE(NULLIF(connection_options->>'migration_namespace',''),$2)::text)) WHERE id=$1 RETURNING connection_options->>'migration_namespace'`, id, candidate).Scan(&namespace)
	return namespace, classify(err)
}

func (d *Store) DisableDatabaseMetadata(ctx context.Context, id int64, now time.Time) error {
	return d.Transaction(ctx, func(tx *Store) error {
		if err := affected(tx.q.ExecContext(ctx, `UPDATE managed_databases SET is_active=FALSE,updated_at=CURRENT_TIMESTAMP WHERE id=$1`, id)); err != nil {
			return err
		}
		return tx.RevokeDatabaseSessions(ctx, id, now)
	})
}

func (d *Store) EffectivePermission(ctx context.Context, user, database int64) (Permission, error) {
	var level int
	err := d.q.QueryRowContext(ctx, `SELECT COALESCE(MAX(CASE p.access_level WHEN 'view_edit' THEN 2 WHEN 'view_only' THEN 1 ELSE 0 END),0)
	 FROM users u JOIN user_groups m ON m.user_id=u.id JOIN group_database_permissions p ON p.group_id=m.group_id
	 JOIN managed_databases d ON d.id=p.database_id WHERE u.id=$1 AND u.is_active AND d.id=$2 AND d.is_active`, user, database).Scan(&level)
	if err != nil {
		return NoAccess, classify(err)
	}
	return []Permission{NoAccess, ViewOnly, ViewEdit}[level], nil
}

func (d *Store) AccessibleDatabases(ctx context.Context, user int64) ([]Database, error) {
	rows, err := d.q.QueryContext(ctx, `SELECT d.id,d.display_name,d.dbms_type,CASE MAX(CASE p.access_level WHEN 'view_edit' THEN 2 ELSE 1 END) WHEN 2 THEN 'view_edit' ELSE 'view_only' END
	 FROM managed_databases d JOIN group_database_permissions p ON p.database_id=d.id JOIN user_groups m ON m.group_id=p.group_id JOIN users u ON u.id=m.user_id
	 WHERE u.id=$1 AND u.is_active AND d.is_active GROUP BY d.id,d.display_name ORDER BY d.display_name`, user)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	result := make([]Database, 0)
	for rows.Next() {
		var database Database
		if err := rows.Scan(&database.ID, &database.Name, &database.DBMS, &database.Permission); err != nil {
			return nil, classify(err)
		}
		result = append(result, database)
	}
	return result, classify(rows.Err())
}

func (d *Store) Databases(ctx context.Context) ([]Database, error) {
	rows, err := d.q.QueryContext(ctx, `SELECT id,display_name,dbms_type,host,port,database_name,is_active,COALESCE(connection_options->>'sslmode','require') FROM managed_databases ORDER BY id`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	result := make([]Database, 0)
	for rows.Next() {
		var database Database
		if err := rows.Scan(&database.ID, &database.Name, &database.DBMS, &database.Host, &database.Port, &database.DatabaseName, &database.Active, &database.SSLMode); err != nil {
			return nil, classify(err)
		}
		result = append(result, database)
	}
	return result, classify(rows.Err())
}

func (d *Store) Administration(ctx context.Context) (AdminData, error) {
	var data AdminData
	var err error
	if data.Users, err = d.Users(ctx); err != nil {
		return data, err
	}
	if data.Groups, err = d.Groups(ctx); err != nil {
		return data, err
	}
	if data.Memberships, err = d.Memberships(ctx); err != nil {
		return data, err
	}
	if data.Databases, err = d.Databases(ctx); err != nil {
		return data, err
	}
	data.Grants, err = d.Grants(ctx)
	return data, err
}
