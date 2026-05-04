package queries

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type Store struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Store {
	return &Store{pool: pool}
}

func (s *Store) GetAllAdminUsers(ctx context.Context) ([]AdminUser, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT id, username, token_hash, role, created_at FROM admin_users`)
	if err != nil {
		return nil, fmt.Errorf("query admin users: %w", err)
	}
	defer rows.Close()

	var users []AdminUser
	for rows.Next() {
		var u AdminUser
		if err := rows.Scan(&u.ID, &u.Username, &u.TokenHash, &u.Role, &u.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan admin user: %w", err)
		}
		users = append(users, u)
	}
	return users, rows.Err()
}

func (s *Store) CreateTenant(ctx context.Context, id uuid.UUID, name, secretKeyHash string, createdBy uuid.UUID) (*Tenant, error) {
	var t Tenant
	err := s.pool.QueryRow(ctx,
		`INSERT INTO tenants (id, name, secret_key_hash, created_by)
		 VALUES ($1, $2, $3, $4)
		 RETURNING id, name, secret_key_hash, is_active, created_at, created_by`,
		id, name, secretKeyHash, createdBy,
	).Scan(&t.ID, &t.Name, &t.SecretKeyHash, &t.IsActive, &t.CreatedAt, &t.CreatedBy)
	if err != nil {
		return nil, fmt.Errorf("create tenant: %w", err)
	}
	return &t, nil
}

func (s *Store) CreateAdminUser(ctx context.Context, id uuid.UUID, username, tokenHash, role string) error {
	_, err := s.pool.Exec(ctx,
		`INSERT INTO admin_users (id, username, token_hash, role)
		 VALUES ($1, $2, $3, $4)
		 ON CONFLICT (username) DO NOTHING`,
		id, username, tokenHash, role,
	)
	if err != nil {
		return fmt.Errorf("create admin user: %w", err)
	}
	return nil
}

func (s *Store) AdminUserCount(ctx context.Context) (int64, error) {
	var count int64
	err := s.pool.QueryRow(ctx, `SELECT COUNT(*) FROM admin_users`).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count admin users: %w", err)
	}
	return count, nil
}
