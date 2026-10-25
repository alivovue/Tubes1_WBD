package dao

import "context"

func (d *Store) GroupPermission(ctx context.Context, group, database int64) (Permission, error) {
	var permission Permission
	err := d.q.QueryRowContext(ctx, `SELECT COALESCE((SELECT access_level FROM group_database_permissions WHERE group_id=$1 AND database_id=$2),'')`, group, database).Scan(&permission)
	return permission, classify(err)
}

func (d *Store) SaveGroup(ctx context.Context, id int64, name, description string) error {
	if id == 0 {
		_, err := d.q.ExecContext(ctx, `INSERT INTO access_groups(name,description) VALUES($1,$2)`, name, description)
		return classify(err)
	}
	return affected(d.q.ExecContext(ctx, `UPDATE access_groups SET name=$2,description=$3 WHERE id=$1`, id, name, description))
}

func (d *Store) DeleteGroup(ctx context.Context, id int64) error {
	return d.Transaction(ctx, func(tx *Store) error {
		if _, err := tx.q.ExecContext(ctx, `DELETE FROM group_database_permissions WHERE group_id=$1`, id); err != nil {
			return err
		}
		if _, err := tx.q.ExecContext(ctx, `DELETE FROM user_groups WHERE group_id=$1`, id); err != nil {
			return err
		}
		return affected(tx.q.ExecContext(ctx, `DELETE FROM access_groups WHERE id=$1`, id))
	})
}

func (d *Store) SetMembership(ctx context.Context, user, group int64, member bool) error {
	var exists bool
	if err := d.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM users u CROSS JOIN access_groups g WHERE u.id=$1 AND g.id=$2)`, user, group).Scan(&exists); err != nil {
		return classify(err)
	}
	if !exists {
		return ErrNotFound
	}
	var err error
	if member {
		_, err = d.q.ExecContext(ctx, `INSERT INTO user_groups(user_id,group_id) VALUES($1,$2) ON CONFLICT DO NOTHING`, user, group)
	} else {
		_, err = d.q.ExecContext(ctx, `DELETE FROM user_groups WHERE user_id=$1 AND group_id=$2`, user, group)
	}
	return classify(err)
}

func (d *Store) SetGrant(ctx context.Context, actor, group, database int64, permission Permission) error {
	if permission != NoAccess && permission != ViewOnly && permission != ViewEdit {
		return ErrInvalid
	}
	var exists bool
	if err := d.q.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM access_groups g CROSS JOIN managed_databases d WHERE g.id=$1 AND d.id=$2)`, group, database).Scan(&exists); err != nil {
		return classify(err)
	}
	if !exists {
		return ErrNotFound
	}
	var err error
	if permission == NoAccess {
		_, err = d.q.ExecContext(ctx, `DELETE FROM group_database_permissions WHERE group_id=$1 AND database_id=$2`, group, database)
	} else {
		_, err = d.q.ExecContext(ctx, `INSERT INTO group_database_permissions(group_id,database_id,access_level,granted_by) VALUES($1,$2,$3,$4) ON CONFLICT(group_id,database_id) DO UPDATE SET access_level=EXCLUDED.access_level,granted_by=EXCLUDED.granted_by,granted_at=CURRENT_TIMESTAMP`, group, database, permission, actor)
	}
	return classify(err)
}

func (d *Store) Groups(ctx context.Context) ([]Group, error) {
	rows, err := d.q.QueryContext(ctx, `SELECT id,name,COALESCE(description,'') FROM access_groups ORDER BY name`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	result := make([]Group, 0)
	for rows.Next() {
		var group Group
		if err := rows.Scan(&group.ID, &group.Name, &group.Description); err != nil {
			return nil, classify(err)
		}
		result = append(result, group)
	}
	return result, classify(rows.Err())
}

func (d *Store) Memberships(ctx context.Context) ([]Membership, error) {
	rows, err := d.q.QueryContext(ctx, `SELECT user_id,group_id FROM user_groups ORDER BY user_id,group_id`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	result := make([]Membership, 0)
	for rows.Next() {
		var membership Membership
		if err := rows.Scan(&membership.UserID, &membership.GroupID); err != nil {
			return nil, classify(err)
		}
		result = append(result, membership)
	}
	return result, classify(rows.Err())
}

func (d *Store) Grants(ctx context.Context) ([]Grant, error) {
	rows, err := d.q.QueryContext(ctx, `SELECT group_id,database_id,access_level FROM group_database_permissions ORDER BY group_id,database_id`)
	if err != nil {
		return nil, classify(err)
	}
	defer rows.Close()
	result := make([]Grant, 0)
	for rows.Next() {
		var grant Grant
		if err := rows.Scan(&grant.GroupID, &grant.DatabaseID, &grant.Permission); err != nil {
			return nil, classify(err)
		}
		result = append(result, grant)
	}
	return result, classify(rows.Err())
}
