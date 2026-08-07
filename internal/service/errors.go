package service

import "errors"

var (
	ErrValidation         = errors.New("validation error")
	ErrDuplicateUser      = errors.New("user already exists")
	ErrDuplicateUnitDT    = errors.New("unit already exists")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrInactiveUser       = errors.New("user inactive")
	ErrConflict           = errors.New("conflict")
	ErrNoClockIn          = errors.New("clock in not found")
	ErrAlreadyClockedOut  = errors.New("already clocked out")
	ErrInvalidLocation    = errors.New("invalid location")
	ErrInvalidPhoto       = errors.New("invalid photo")
)
