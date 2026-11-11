package dao

import (
	"context"
	"encoding/json"
)

const reviewSelect = `SELECT v.*,COALESCE((SELECT jsonb_agg(a ORDER BY a.group_id) FROM migration_approvals a WHERE a.review_id=v.id),'[]'::jsonb) AS decisions FROM migration_reviews v`

func (d *Store) Review(ctx context.Context, id int64, lock bool) (MigrationReview, error) {
	if lock {
		if !d.inTx {
			return MigrationReview{}, ErrInvalid
		}
		var found int64
		if err := d.q.QueryRowContext(ctx, `SELECT id FROM migration_reviews WHERE id=$1 FOR UPDATE`, id).Scan(&found); err != nil {
			return MigrationReview{}, classify(err)
		}
	}
	var r MigrationReview
	err := d.workflowOne(ctx, &r, `SELECT row_to_json(x) FROM (`+reviewSelect+` WHERE v.id=$1) x`, id)
	return r, err
}
func (d *Store) Reviews(ctx context.Context, database int64, limit, offset int) ([]MigrationReview, error) {
	return workflowList[MigrationReview](ctx, d, `SELECT row_to_json(x) FROM (`+reviewSelect+` WHERE v.database_id=$1 ORDER BY v.id DESC LIMIT $2 OFFSET $3) x`, database, limit, offset)
}
func (d *Store) CreateReview(ctx context.Context, r MigrationReview) (MigrationReview, error) {
	raw, err := json.Marshal(r.AuthoritySnapshot)
	if err != nil {
		return r, err
	}
	err = d.q.QueryRowContext(ctx, `INSERT INTO migration_reviews(revision_id,database_id,action,target_execution_id,target_job_id,authority_revision,authority_snapshot,connection_fingerprint,expected_generation,expected_active_version,recovery_fingerprint,action_hash,submitted_by) VALUES($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13) RETURNING id`, r.RevisionID, r.DatabaseID, r.Action, r.TargetExecutionID, r.TargetJobID, r.AuthorityRevision, string(raw), r.ConnectionFingerprint, r.ExpectedGeneration, r.ExpectedActiveVersion, r.RecoveryFingerprint, r.ActionHash, r.SubmittedBy).Scan(&r.ID)
	if err != nil {
		return r, classify(err)
	}
	for _, g := range r.AuthoritySnapshot.Groups {
		if _, err = d.q.ExecContext(ctx, `INSERT INTO migration_review_required_groups(review_id,group_id,leader_user_id_snapshot,access_level) VALUES($1,$2,$3,$4)`, r.ID, g.ID, g.LeaderID, g.Permission); err != nil {
			return r, classify(err)
		}
	}
	return d.Review(ctx, r.ID, false)
}
func (d *Store) SetReviewState(ctx context.Context, id int64, state string) error {
	return affected(d.q.ExecContext(ctx, `UPDATE migration_reviews SET review_state=$2 WHERE id=$1`, id, state))
}
func (d *Store) AddDecision(ctx context.Context, review int64, a MigrationDecision) error {
	_, err := d.q.ExecContext(ctx, `INSERT INTO migration_approvals(review_id,group_id,approver_user_id,decision,note) VALUES($1,$2,$3,$4,$5)`, review, a.GroupID, a.ApproverID, a.Decision, a.Note)
	return classify(err)
}
func (d *Store) MarkReviewQueued(ctx context.Context, id int64, job, key string) error {
	return affected(d.q.ExecContext(ctx, `UPDATE migration_reviews SET queued_job_id=$2,idempotency_key=$3 WHERE id=$1 AND review_state='approved' AND queued_job_id IS NULL`, id, job, key))
}

func (d *Store) ReviewForJob(ctx context.Context, job string) (MigrationReview, string, error) {
	var review int64
	var status string
	err := d.q.QueryRowContext(ctx, `SELECT review_id,status FROM migration_jobs WHERE id=$1`, job).Scan(&review, &status)
	if err != nil {
		return MigrationReview{}, "", classify(err)
	}
	r, err := d.Review(ctx, review, false)
	return r, status, err
}
