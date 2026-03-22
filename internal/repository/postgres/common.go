package postgres

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"stageflow/internal/domain"
)

type sqlStateCarrier interface {
	SQLState() string
}

func beginTx(ctx context.Context, db *sql.DB) (*sql.Tx, error) {
	tx, err := db.BeginTx(ctx, &sql.TxOptions{})
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	return tx, nil
}

func marshalJSON(value any, field string) ([]byte, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", field, err)
	}
	return payload, nil
}

func unmarshalJSON[T any](raw []byte, field string) (T, error) {
	var out T
	if len(raw) == 0 {
		return out, nil
	}
	if err := json.Unmarshal(raw, &out); err != nil {
		return out, fmt.Errorf("unmarshal %s: %w", field, err)
	}
	return out, nil
}

func normalizeRunTimestamps(run domain.FlowRun) domain.FlowRun {
	now := time.Now().UTC()
	if run.CreatedAt.IsZero() {
		run.CreatedAt = now
	}
	if run.UpdatedAt.IsZero() {
		run.UpdatedAt = run.CreatedAt
	}
	return run
}

func normalizeRunStepTimestamps(step domain.FlowRunStep) domain.FlowRunStep {
	now := time.Now().UTC()
	if step.CreatedAt.IsZero() {
		step.CreatedAt = now
	}
	if step.UpdatedAt.IsZero() {
		step.UpdatedAt = step.CreatedAt
	}
	return step
}

func mapDBError(err error, entity string) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		return &domain.NotFoundError{Entity: entity}
	}
	var stateErr sqlStateCarrier
	if errors.As(err, &stateErr) {
		switch stateErr.SQLState() {
		case "23505":
			return &domain.ConflictError{Entity: entity}
		case "23503":
			return &domain.NotFoundError{Entity: entity}
		}
	}
	return err
}
