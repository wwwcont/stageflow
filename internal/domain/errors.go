package domain

import (
	"errors"
	"fmt"
)

var (
	ErrValidation       = errors.New("validation error")
	ErrAssertionFailure = errors.New("assertion failure")
	ErrTransport        = errors.New("transport error")
	ErrExecution        = errors.New("execution error")
	ErrNotFound         = errors.New("not found")
	ErrConflict         = errors.New("conflict")
)

type ValidationError struct {
	Message string
	Cause   error
}

func (e *ValidationError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return fmt.Sprintf("%s: %s", ErrValidation, e.Message)
	}
	return fmt.Sprintf("%s: %s: %v", ErrValidation, e.Message, e.Cause)
}

func (e *ValidationError) Unwrap() error {
	if e == nil {
		return ErrValidation
	}
	return errors.Join(ErrValidation, e.Cause)
}

type AssertionFailureError struct {
	RuleName string
	Message  string
}

func (e *AssertionFailureError) Error() string {
	if e == nil {
		return ""
	}
	if e.RuleName == "" {
		return fmt.Sprintf("%s: %s", ErrAssertionFailure, e.Message)
	}
	return fmt.Sprintf("%s: %s: %s", ErrAssertionFailure, e.RuleName, e.Message)
}

func (e *AssertionFailureError) Unwrap() error {
	return ErrAssertionFailure
}

type TransportError struct {
	Operation string
	Cause     error
}

func (e *TransportError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return fmt.Sprintf("%s: %s", ErrTransport, e.Operation)
	}
	return fmt.Sprintf("%s: %s: %v", ErrTransport, e.Operation, e.Cause)
}

func (e *TransportError) Unwrap() error {
	return errors.Join(ErrTransport, e.Cause)
}

type ExecutionError struct {
	Operation string
	Cause     error
}

func (e *ExecutionError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return fmt.Sprintf("%s: %s", ErrExecution, e.Operation)
	}
	return fmt.Sprintf("%s: %s: %v", ErrExecution, e.Operation, e.Cause)
}

func (e *ExecutionError) Unwrap() error {
	return errors.Join(ErrExecution, e.Cause)
}

type NotFoundError struct {
	Entity string
	ID     string
}

func (e *NotFoundError) Error() string {
	if e == nil {
		return ""
	}
	if e.ID == "" {
		return fmt.Sprintf("%s: %s", ErrNotFound, e.Entity)
	}
	return fmt.Sprintf("%s: %s %q", ErrNotFound, e.Entity, e.ID)
}

func (e *NotFoundError) Unwrap() error {
	return ErrNotFound
}

type ConflictError struct {
	Entity string
	Field  string
	Value  string
}

func (e *ConflictError) Error() string {
	if e == nil {
		return ""
	}
	if e.Field == "" {
		return fmt.Sprintf("%s: %s", ErrConflict, e.Entity)
	}
	return fmt.Sprintf("%s: %s %s=%q", ErrConflict, e.Entity, e.Field, e.Value)
}

func (e *ConflictError) Unwrap() error {
	return ErrConflict
}
