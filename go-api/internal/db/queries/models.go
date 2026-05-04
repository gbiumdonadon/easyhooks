package queries

import (
	"time"

	"github.com/google/uuid"
)

type AdminUser struct {
	ID        uuid.UUID `db:"id"`
	Username  string    `db:"username"`
	TokenHash string    `db:"token_hash"`
	Role      string    `db:"role"`
	CreatedAt time.Time `db:"created_at"`
}

type Tenant struct {
	ID            uuid.UUID `db:"id"`
	Name          string    `db:"name"`
	SecretKeyHash string    `db:"secret_key_hash"`
	IsActive      bool      `db:"is_active"`
	CreatedAt     time.Time `db:"created_at"`
	CreatedBy     uuid.UUID `db:"created_by"`
}
