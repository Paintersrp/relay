package store

import (
	"context"
	"database/sql"
	"fmt"

	"relay/internal/store/generated"
)

type CreateProjectContextRecordParams = generated.CreateProjectContextRecordParams
type GetProjectContextRecordByRecordIDParams = generated.GetProjectContextRecordByRecordIDParams
type GetActiveProjectContextRecordByBodyHashParams = generated.GetActiveProjectContextRecordByBodyHashParams
type ListProjectContextRecordsParams = generated.ListProjectContextRecordsParams
type SearchProjectContextRecordsParams = generated.SearchProjectContextRecordsParams
type MarkProjectContextRecordSupersededParams = generated.MarkProjectContextRecordSupersededParams

type SupersedeProjectContextRecordParams struct {
	Create  CreateProjectContextRecordParams
	MarkOld MarkProjectContextRecordSupersededParams
}

type SupersedeProjectContextRecordResult struct {
	Old ProjectContextRecord
	New ProjectContextRecord
}

func (s *Store) CreateProjectContextRecord(ctx context.Context, params CreateProjectContextRecordParams) (*ProjectContextRecord, error) {
	record, err := s.queries.CreateProjectContextRecord(ctx, params)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Store) GetProjectContextRecordByRecordID(ctx context.Context, params GetProjectContextRecordByRecordIDParams) (*ProjectContextRecord, error) {
	record, err := s.queries.GetProjectContextRecordByRecordID(ctx, params)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Store) GetActiveProjectContextRecordByBodyHash(ctx context.Context, params GetActiveProjectContextRecordByBodyHashParams) (*ProjectContextRecord, error) {
	record, err := s.queries.GetActiveProjectContextRecordByBodyHash(ctx, params)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Store) ListProjectContextRecords(ctx context.Context, params ListProjectContextRecordsParams) ([]ProjectContextRecord, error) {
	return s.queries.ListProjectContextRecords(ctx, params)
}

func (s *Store) SearchProjectContextRecords(ctx context.Context, params SearchProjectContextRecordsParams) ([]ProjectContextRecord, error) {
	return s.queries.SearchProjectContextRecords(ctx, params)
}

func (s *Store) MarkProjectContextRecordSuperseded(ctx context.Context, params MarkProjectContextRecordSupersededParams) (*ProjectContextRecord, error) {
	record, err := s.queries.MarkProjectContextRecordSuperseded(ctx, params)
	if err != nil {
		return nil, err
	}
	return &record, nil
}

func (s *Store) SupersedeProjectContextRecord(ctx context.Context, params SupersedeProjectContextRecordParams) (*SupersedeProjectContextRecordResult, error) {
	tx, err := s.db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()

	qtx := s.queries.WithTx(tx)
	newRecord, err := qtx.CreateProjectContextRecord(ctx, params.Create)
	if err != nil {
		return nil, err
	}
	params.MarkOld.SupersededByRecordID = newRecord.ContextRecordID
	oldRecord, err := qtx.MarkProjectContextRecordSuperseded(ctx, params.MarkOld)
	if err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, fmt.Errorf("commit project context supersession: %w", err)
	}
	return &SupersedeProjectContextRecordResult{Old: oldRecord, New: newRecord}, nil
}
