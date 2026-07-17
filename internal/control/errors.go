package control

import "errors"

var (
	ErrNotFound      = errors.New("resource not found")
	ErrConflict      = errors.New("resource conflict")
	ErrVersion       = errors.New("version conflict")
	ErrResourceInUse = errors.New("resource is in use")
	ErrValidation    = errors.New("validation failed")
)

type ValidationError struct {
	Field   string
	Message string
}

func (e ValidationError) Error() string { return e.Field + ": " + e.Message }
func (e ValidationError) Unwrap() error { return ErrValidation }
