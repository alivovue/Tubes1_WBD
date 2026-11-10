package dbmanagement

import (
	"context"
	"database/sql"
	"errors"

	"tubes1_wbd/backend/access"
	"tubes1_wbd/backend/dbmanagement/adapters"
)

type Schema = adapters.Schema
type Table = adapters.Table
type Column = adapters.Column
type Constraint = adapters.Constraint
type Index = adapters.Index
type TableDetail = adapters.TableDetail
type SortColumn = adapters.SortColumn
type PageOptions = adapters.PageOptions
type DataPage = adapters.DataPage
type DataColumn = adapters.DataColumn
type Snapshot = adapters.Snapshot
type Plan = adapters.Plan

var ErrStaleSnapshot = errors.New("schema changed while reading; retry the operation")

func (s *Service) read(ctx context.Context, actor, database int64, permission access.Permission, action func(context.Context, adapters.Querier, adapters.Adapter) error) error {
	ctx, cancel := context.WithTimeout(ctx, s.config.QueryTimeout)
	defer cancel()
	target, err := s.open(ctx, actor, database, permission)
	if err != nil {
		return err
	}
	defer target.DB.Close()
	tx, err := target.DB.BeginTx(ctx, &sql.TxOptions{ReadOnly: true, Isolation: sql.LevelRepeatableRead})
	if err != nil {
		return ErrConnection
	}
	defer tx.Rollback()
	if err := action(ctx, tx, target.Adapter); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return ErrQuery
	}
	return nil
}
func (s *Service) Schemas(ctx context.Context, actor, database int64) ([]Schema, error) {
	var value []Schema
	err := s.read(ctx, actor, database, access.ViewOnly, func(ctx context.Context, q adapters.Querier, a adapters.Adapter) error {
		var err error
		value, err = a.Schemas(ctx, q)
		return err
	})
	return value, err
}
func (s *Service) Tables(ctx context.Context, actor, database int64, schema string) ([]Table, error) {
	var value []Table
	err := s.read(ctx, actor, database, access.ViewOnly, func(ctx context.Context, q adapters.Querier, a adapters.Adapter) error {
		var err error
		value, err = a.Tables(ctx, q, schema)
		return err
	})
	return value, err
}
func (s *Service) DescribeTable(ctx context.Context, actor, database int64, schema, table string) (TableDetail, error) {
	var value TableDetail
	err := s.read(ctx, actor, database, access.ViewOnly, func(ctx context.Context, q adapters.Querier, a adapters.Adapter) error {
		var err error
		value, err = a.DescribeTable(ctx, q, schema, table)
		return err
	})
	return value, err
}
func (s *Service) TableData(ctx context.Context, actor, database int64, schema, table string, options PageOptions) (DataPage, error) {
	var value DataPage
	err := s.read(ctx, actor, database, access.ViewOnly, func(ctx context.Context, q adapters.Querier, a adapters.Adapter) error {
		var err error
		value, err = a.TableData(ctx, q, schema, table, options)
		return err
	})
	return value, err
}
func (s *Service) Fingerprint(ctx context.Context, actor, database int64) (string, error) {
	var value string
	err := s.read(ctx, actor, database, access.ViewOnly, func(ctx context.Context, q adapters.Querier, a adapters.Adapter) error {
		var err error
		value, err = a.Fingerprint(ctx, q)
		return err
	})
	return value, err
}
func (s *Service) Snapshot(ctx context.Context, actor, database int64) (Snapshot, error) {
	var value Snapshot
	err := s.read(ctx, actor, database, access.ViewOnly, func(ctx context.Context, q adapters.Querier, a adapters.Adapter) error {
		before, err := a.Fingerprint(ctx, q)
		if err != nil {
			return err
		}
		value, err = a.Snapshot(ctx, q)
		if err != nil {
			return err
		}
		after, err := a.Fingerprint(ctx, q)
		if err != nil {
			return err
		}
		if before != after {
			return ErrStaleSnapshot
		}
		value.Fingerprint = after
		return nil
	})
	return value, err
}
func (s *Service) QueryPlans(ctx context.Context, actor, database int64, query string) ([]Plan, error) {
	var value []Plan
	err := s.read(ctx, actor, database, access.ViewEdit, func(ctx context.Context, q adapters.Querier, a adapters.Adapter) error {
		statements, err := adapters.ParseSQL(query, a.Name())
		if err != nil {
			return err
		}
		value, err = a.QueryPlans(ctx, q, statements)
		return err
	})
	return value, err
}
