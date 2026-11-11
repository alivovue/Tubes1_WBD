package dao

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"

	"schakker_shared/authority"
)

func (d *Store) SetGroupLeader(ctx context.Context, group, user int64) error {
	return affected(d.q.ExecContext(ctx, `UPDATE access_groups SET leader_user_id=$2 WHERE id=$1
 AND EXISTS(SELECT 1 FROM user_groups m JOIN users u ON u.id=m.user_id
 WHERE m.group_id=$1 AND m.user_id=$2 AND u.is_active)`, group, user))
}

// CreateLedGroup adds the first membership and designation in the same transaction.
func (d *Store) CreateLedGroup(ctx context.Context, name, description string, leader int64) (int64, error) {
	var id int64
	err := d.Transaction(ctx, func(tx *Store) error {
		if err := tx.q.QueryRowContext(ctx, `INSERT INTO access_groups(name,description) SELECT $1,$2
   WHERE EXISTS(SELECT 1 FROM users WHERE id=$3 AND is_active) RETURNING id`, name, description, leader).Scan(&id); err != nil {
			return classify(err)
		}
		if err := tx.SetMembership(ctx, leader, id, true); err != nil {
			return err
		}
		return tx.SetGroupLeader(ctx, id, leader)
	})
	return id, err
}

// MigrationAuthority reads version, groups, leader validity and connection identity
// in one PostgreSQL statement, so concurrent changes cannot produce a torn snapshot.
func (d *Store) MigrationAuthority(ctx context.Context, actor, database int64) (authority.Snapshot, error) {
	var snapshot authority.Snapshot
	var groups []byte
	var connection string
	err := d.q.QueryRowContext(ctx, `SELECT d.id,c.revision,
 jsonb_build_array(d.dbms_type,d.host,d.port,d.database_name,d.db_username,
 encode(d.encrypted_password,'hex'),d.connection_options,d.is_active)::text,
 COALESCE((SELECT jsonb_agg(jsonb_build_object('group_id',g.id,'name',g.name,
 'leader_user_id',COALESCE(g.leader_user_id,0),'leader_name',COALESCE(l.display_name,''),
 'access_level',p.access_level,'leader_valid',COALESCE(l.is_active AND EXISTS
 (SELECT 1 FROM user_groups m WHERE m.group_id=g.id AND m.user_id=l.id),FALSE)) ORDER BY g.id)
 FROM group_database_permissions p JOIN access_groups g ON g.id=p.group_id
 LEFT JOIN users l ON l.id=g.leader_user_id WHERE p.database_id=d.id),'[]'::jsonb)
 FROM managed_databases d CROSS JOIN migration_authority_clock c
 WHERE d.id=$2 AND d.is_active AND c.singleton
 AND EXISTS(SELECT 1 FROM users WHERE id=$1 AND is_active)
 AND EXISTS(SELECT 1 FROM user_groups m JOIN group_database_permissions p ON p.group_id=m.group_id
 WHERE m.user_id=$1 AND p.database_id=d.id)`, actor, database).Scan(&snapshot.DatabaseID, &snapshot.Revision, &connection, &groups)
	if err != nil {
		return snapshot, classify(err)
	}
	if err = json.Unmarshal(groups, &snapshot.Groups); err != nil {
		return snapshot, err
	}
	sum := sha256.Sum256([]byte(connection))
	snapshot.ConnectionFingerprint = hex.EncodeToString(sum[:])
	return snapshot, nil
}
