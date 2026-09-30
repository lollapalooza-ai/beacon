package state

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog"
	_ "modernc.org/sqlite"
)

// SQLiteStore implements the Store interface using SQLite.
type SQLiteStore struct {
	db     *sql.DB
	logger zerolog.Logger
}

// NewSQLiteStore creates a new SQLiteStore connected to the given database path.
func NewSQLiteStore(dbPath string, logger zerolog.Logger) (*SQLiteStore, error) {
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite database: %w", err)
	}

	return &SQLiteStore{
		db:     db,
		logger: logger.With().Str("component", "sqlite_store").Logger(),
	}, nil
}

// Initialize creates tables if they do not exist.
func (s *SQLiteStore) Initialize(ctx context.Context) error {
	s.logger.Info().Msg("Initializing database schema")

	query := `
	CREATE TABLE IF NOT EXISTS workloads (
		id TEXT PRIMARY KEY,
		intent TEXT,
		state TEXT,
		requirements TEXT,
		selected_offer TEXT,
		instance_id TEXT,
		instance_provider TEXT,
		instance_region TEXT,
		payment_token_id TEXT,
		budget REAL,
		amount_spent REAL,
		error TEXT,
		created_at TIMESTAMP,
		updated_at TIMESTAMP
	);

	CREATE TABLE IF NOT EXISTS workload_events (
		id TEXT PRIMARY KEY,
		workload_id TEXT,
		event_type TEXT,
		details TEXT,
		created_at TIMESTAMP,
		FOREIGN KEY(workload_id) REFERENCES workloads(id)
	);
	`

	_, err := s.db.ExecContext(ctx, query)
	if err != nil {
		return fmt.Errorf("failed to initialize schema: %w", err)
	}

	return nil
}

// CreateWorkload persists a new workload.
func (s *SQLiteStore) CreateWorkload(ctx context.Context, workload *Workload) error {
	query := `
	INSERT INTO workloads (
		id, intent, state, requirements, selected_offer, instance_id,
		instance_provider, instance_region, payment_token_id, budget,
		amount_spent, error, created_at, updated_at
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`

	var reqStr, offerStr string
	if len(workload.Requirements) > 0 {
		reqStr = string(workload.Requirements)
	}
	if len(workload.SelectedOffer) > 0 {
		offerStr = string(workload.SelectedOffer)
	}

	now := time.Now()
	workload.CreatedAt = now
	workload.UpdatedAt = now

	_, err := s.db.ExecContext(ctx, query,
		workload.ID, workload.Intent, string(workload.State), reqStr, offerStr,
		workload.InstanceID, workload.InstanceProvider, workload.InstanceRegion,
		workload.PaymentTokenID, workload.Budget, workload.AmountSpent,
		workload.Error, workload.CreatedAt, workload.UpdatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to create workload: %w", err)
	}

	return nil
}

// UpdateWorkload updates an existing workload's state.
func (s *SQLiteStore) UpdateWorkload(ctx context.Context, workload *Workload) error {
	query := `
	UPDATE workloads SET
		intent = ?, state = ?, requirements = ?, selected_offer = ?,
		instance_id = ?, instance_provider = ?, instance_region = ?,
		payment_token_id = ?, budget = ?, amount_spent = ?, error = ?,
		updated_at = ?
	WHERE id = ?
	`

	var reqStr, offerStr string
	if len(workload.Requirements) > 0 {
		reqStr = string(workload.Requirements)
	}
	if len(workload.SelectedOffer) > 0 {
		offerStr = string(workload.SelectedOffer)
	}

	workload.UpdatedAt = time.Now()

	res, err := s.db.ExecContext(ctx, query,
		workload.Intent, string(workload.State), reqStr, offerStr,
		workload.InstanceID, workload.InstanceProvider, workload.InstanceRegion,
		workload.PaymentTokenID, workload.Budget, workload.AmountSpent,
		workload.Error, workload.UpdatedAt, workload.ID,
	)

	if err != nil {
		return fmt.Errorf("failed to update workload: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return errors.New("workload not found")
	}

	return nil
}

// GetWorkload retrieves a workload by ID.
func (s *SQLiteStore) GetWorkload(ctx context.Context, id string) (*Workload, error) {
	query := `
	SELECT id, intent, state, requirements, selected_offer, instance_id,
		instance_provider, instance_region, payment_token_id, budget,
		amount_spent, error, created_at, updated_at
	FROM workloads WHERE id = ?
	`

	row := s.db.QueryRowContext(ctx, query, id)

	var w Workload
	var stateStr, reqStr, offerStr string

	err := row.Scan(
		&w.ID, &w.Intent, &stateStr, &reqStr, &offerStr, &w.InstanceID,
		&w.InstanceProvider, &w.InstanceRegion, &w.PaymentTokenID, &w.Budget,
		&w.AmountSpent, &w.Error, &w.CreatedAt, &w.UpdatedAt,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, errors.New("workload not found")
		}
		return nil, fmt.Errorf("failed to get workload: %w", err)
	}

	w.State = WorkloadState(stateStr)
	if reqStr != "" {
		w.Requirements = json.RawMessage(reqStr)
	}
	if offerStr != "" {
		w.SelectedOffer = json.RawMessage(offerStr)
	}

	return &w, nil
}

// ListActiveWorkloads returns all workloads that are not in a terminal state.
func (s *SQLiteStore) ListActiveWorkloads(ctx context.Context) ([]*Workload, error) {
	query := `
	SELECT id, intent, state, requirements, selected_offer, instance_id,
		instance_provider, instance_region, payment_token_id, budget,
		amount_spent, error, created_at, updated_at
	FROM workloads 
	WHERE state NOT IN (?, ?)
	`

	rows, err := s.db.QueryContext(ctx, query, string(StateCompleted), string(StateFailed))
	if err != nil {
		return nil, fmt.Errorf("failed to list active workloads: %w", err)
	}
	defer rows.Close()

	var workloads []*Workload
	for rows.Next() {
		var w Workload
		var stateStr, reqStr, offerStr string

		err := rows.Scan(
			&w.ID, &w.Intent, &stateStr, &reqStr, &offerStr, &w.InstanceID,
			&w.InstanceProvider, &w.InstanceRegion, &w.PaymentTokenID, &w.Budget,
			&w.AmountSpent, &w.Error, &w.CreatedAt, &w.UpdatedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("failed to scan workload: %w", err)
		}

		w.State = WorkloadState(stateStr)
		if reqStr != "" {
			w.Requirements = json.RawMessage(reqStr)
		}
		if offerStr != "" {
			w.SelectedOffer = json.RawMessage(offerStr)
		}

		workloads = append(workloads, &w)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating active workloads: %w", err)
	}

	return workloads, nil
}

// RecordEvent persists a workload lifecycle event for auditability.
func (s *SQLiteStore) RecordEvent(ctx context.Context, event *WorkloadEvent) error {
	query := `
	INSERT INTO workload_events (id, workload_id, event_type, details, created_at)
	VALUES (?, ?, ?, ?, ?)
	`

	if event.ID == "" {
		event.ID = uuid.New().String()
	}
	if event.CreatedAt.IsZero() {
		event.CreatedAt = time.Now()
	}

	_, err := s.db.ExecContext(ctx, query,
		event.ID, event.WorkloadID, event.EventType, event.Details, event.CreatedAt,
	)

	if err != nil {
		return fmt.Errorf("failed to record event: %w", err)
	}

	return nil
}

// ListEvents retrieves all events for a given workload, ordered by creation time.
func (s *SQLiteStore) ListEvents(ctx context.Context, workloadID string) ([]*WorkloadEvent, error) {
	query := `
	SELECT id, workload_id, event_type, details, created_at
	FROM workload_events
	WHERE workload_id = ?
	ORDER BY created_at ASC
	`

	rows, err := s.db.QueryContext(ctx, query, workloadID)
	if err != nil {
		return nil, fmt.Errorf("failed to list events: %w", err)
	}
	defer rows.Close()

	var events []*WorkloadEvent
	for rows.Next() {
		var e WorkloadEvent
		err := rows.Scan(&e.ID, &e.WorkloadID, &e.EventType, &e.Details, &e.CreatedAt)
		if err != nil {
			return nil, fmt.Errorf("failed to scan event: %w", err)
		}
		events = append(events, &e)
	}

	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating events: %w", err)
	}

	return events, nil
}

// Close closes the store and releases resources.
func (s *SQLiteStore) Close() error {
	return s.db.Close()
}
