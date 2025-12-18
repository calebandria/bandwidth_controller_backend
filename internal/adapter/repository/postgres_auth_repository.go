package adapter

import (
	"bandwidth_controller_backend/internal/core/domain"
	"database/sql"
	"errors"
	"fmt"

	"golang.org/x/crypto/bcrypt"
)

// PostgresAuthRepository implements AuthRepository using PostgreSQL
type PostgresAuthRepository struct {
	db *sql.DB
}

// NewPostgresAuthRepository creates a new PostgreSQL auth repository
func NewPostgresAuthRepository(db *sql.DB) *PostgresAuthRepository {
	return &PostgresAuthRepository{
		db: db,
	}
}

// FindByUsername finds a user by username
func (r *PostgresAuthRepository) FindByUsername(username string) (*domain.User, error) {
	query := `
		SELECT id, username, password_hash, role, name, created_at, updated_at
		FROM users
		WHERE username = $1
	`

	user := &domain.User{}
	err := r.db.QueryRow(query, username).Scan(
		&user.ID,
		&user.Username,
		&user.Password, // This is the password_hash from DB
		&user.Role,
		&user.Name,
		&user.CreatedAt,
		&user.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, errors.New("user not found")
		}
		return nil, fmt.Errorf("database error: %w", err)
	}

	return user, nil
}

// Create creates a new user with hashed password
func (r *PostgresAuthRepository) Create(user *domain.User) error {
	// Hash the password before storing
	hashedPassword, err := bcrypt.GenerateFromPassword([]byte(user.Password), bcrypt.DefaultCost)
	if err != nil {
		return fmt.Errorf("failed to hash password: %w", err)
	}

	query := `
		INSERT INTO users (username, password_hash, role, name)
		VALUES ($1, $2, $3, $4)
		RETURNING id, created_at, updated_at
	`

	err = r.db.QueryRow(
		query,
		user.Username,
		string(hashedPassword),
		user.Role,
		user.Name,
	).Scan(&user.ID, &user.CreatedAt, &user.UpdatedAt)

	if err != nil {
		return fmt.Errorf("failed to create user: %w", err)
	}

	return nil
}

// VerifyPassword checks if the provided password matches the stored hash
func (r *PostgresAuthRepository) VerifyPassword(hashedPassword, plainPassword string) error {
	return bcrypt.CompareHashAndPassword([]byte(hashedPassword), []byte(plainPassword))
}
