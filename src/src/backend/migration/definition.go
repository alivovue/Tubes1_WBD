package migration

import (
	"context"
	"strings"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/database/dao"
)

type DefinitionInput struct {
	Title              string `json:"title"`
	UpSQL              string `json:"up_sql"`
	DownSQL            string `json:"down_sql"`
	Irreversible       bool   `json:"irreversible"`
	IrreversibleReason string `json:"irreversible_reason"`
}

// ValidateDefinition checks both directions without executing target SQL.
func ValidateDefinition(input DefinitionInput) error {
	if strings.TrimSpace(input.Title) == "" || len(input.Title) > 255 || len(input.UpSQL) > 2<<20 || len(input.DownSQL) > 2<<20 {
		return ErrDefinitionInvalid
	}
	if input.Irreversible {
		if input.DownSQL != "" || len(strings.TrimSpace(input.IrreversibleReason)) < 10 || len(input.IrreversibleReason) > 2000 {
			return ErrDefinitionInvalid
		}
	} else if strings.TrimSpace(input.DownSQL) == "" || input.IrreversibleReason != "" {
		return ErrDefinitionInvalid
	}
	if _, err := ValidateSQL(input.UpSQL, "postgresql"); err != nil {
		return ErrDefinitionInvalid
	}
	if !input.Irreversible {
		if _, err := ValidateSQL(input.DownSQL, "postgresql"); err != nil {
			return ErrDefinitionInvalid
		}
	}
	return nil
}
func revisionInput(definition, no int64, in DefinitionInput) dao.MigrationRevision {
	return dao.MigrationRevision{DefinitionID: definition, RevisionNo: no, UpSQL: in.UpSQL, DownSQL: in.DownSQL, Irreversible: in.Irreversible, IrreversibleReason: in.IrreversibleReason, SQLHash: ContentHash(in.UpSQL, in.DownSQL, in.Irreversible, in.IrreversibleReason)}
}
func (w *Workflow) CreateDefinition(ctx context.Context, actor, database int64, in DefinitionInput) (out dao.MigrationRevision, err error) {
	ctx, end := w.observe(ctx, "migration.definition.create", actor, database)
	defer func() { end(err) }()
	if err = w.auth.RequirePermission(ctx, actor, database, access.ViewEdit); err != nil {
		return out, err
	}
	if err = ValidateDefinition(in); err != nil {
		return out, err
	}
	m, err := w.auth.DatabaseConnection(ctx, actor, database, access.ViewEdit)
	if err != nil {
		return out, err
	}
	if m.DBMS != "postgresql" {
		return out, ErrDefinitionInvalid
	}
	err = w.store.Transaction(ctx, func(tx *dao.Store) error {
		if err := tx.LockDatabase(ctx, database); err != nil {
			return err
		}
		blocked, err := tx.BlockingSynchronousHistory(ctx, database)
		if err != nil {
			return err
		}
		if blocked {
			return ErrWorkflowConflict
		}
		id, err := tx.CreateDefinition(ctx, database, actor, strings.TrimSpace(in.Title))
		if err != nil {
			return err
		}
		out, err = tx.CreateRevision(ctx, revisionInput(id, 1, in))
		return err
	})
	return out, err
}
func (w *Workflow) EditDefinition(ctx context.Context, actor, definition, expectedRevision int64, in DefinitionInput) (out dao.MigrationRevision, err error) {
	ctx, end := w.observe(ctx, "migration.definition.edit", actor, 0)
	defer func() { end(err) }()
	previous, err := w.store.LatestRevision(ctx, definition)
	if err != nil {
		return out, err
	}
	if err = w.auth.RequirePermission(ctx, actor, previous.DatabaseID, access.ViewEdit); err != nil {
		return out, err
	}
	if err = ValidateDefinition(in); err != nil {
		return out, err
	}
	err = w.store.Transaction(ctx, func(tx *dao.Store) error {
		if err := tx.LockDefinition(ctx, definition); err != nil {
			return err
		}
		previous, err := tx.LatestRevision(ctx, definition)
		if err != nil {
			return err
		}
		if previous.ID != expectedRevision {
			return ErrWorkflowConflict
		}
		executed, err := tx.DefinitionExecuted(ctx, definition)
		if err != nil {
			return err
		}
		if executed {
			return ErrWorkflowConflict
		}
		if err := tx.InvalidateDefinitionReviews(ctx, definition); err != nil {
			return err
		}
		if err := tx.RenameDefinition(ctx, definition, strings.TrimSpace(in.Title)); err != nil {
			return err
		}
		out, err = tx.CreateRevision(ctx, revisionInput(definition, previous.RevisionNo+1, in))
		return err
	})
	return out, err
}
