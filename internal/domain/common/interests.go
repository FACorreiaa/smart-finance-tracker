package common

import (
	"time"

	"github.com/google/uuid"
)

type Interest struct {
	ID          uuid.UUID  `json:"id"`
	Name        string     `json:"name"`
	Description *string    `json:"description,omitempty"`
	Active      *bool      `json:"active"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   *time.Time `json:"updated_at"`
	Source      string     `json:"source"`
}
