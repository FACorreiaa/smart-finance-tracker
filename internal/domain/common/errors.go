package common

import "errors"

var (
	ErrNotFound        = errors.New("requested item not found")
	ErrConflict        = errors.New("item already exists or conflict")
	ErrUnauthenticated = errors.New("authentication required or invalid credentials")
	ErrForbidden       = errors.New("action forbidden")
	ErrBadRequest      = errors.New("bad request")
)
