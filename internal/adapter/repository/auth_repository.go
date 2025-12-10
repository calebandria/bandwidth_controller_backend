package adapter

import (
	"bandwidth_controller_backend/internal/core/domain"
	"errors"
	"sync"
)

// InMemoryAuthRepository is a simple in-memory storage for users
// In production, use a real database (PostgreSQL, MySQL, etc.)
type InMemoryAuthRepository struct {
	users map[string]*domain.User
	mu    sync.RWMutex
}

func NewInMemoryAuthRepository() *InMemoryAuthRepository {
	repo := &InMemoryAuthRepository{
		users: make(map[string]*domain.User),
	}

	// Initialize with default users
	// In production, load from database and use hashed passwords
	repo.users["admin"] = &domain.User{
		Username: "admin",
		Password: "admin123", // In production: use bcrypt hashed password
		Role:     "admin",
		Name:     "Administrator",
	}

	repo.users["user"] = &domain.User{
		Username: "user",
		Password: "user123", // In production: use bcrypt hashed password
		Role:     "user",
		Name:     "Standard User",
	}

	return repo
}

// FindByUsername finds a user by username
func (r *InMemoryAuthRepository) FindByUsername(username string) (*domain.User, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	user, exists := r.users[username]
	if !exists {
		return nil, errors.New("user not found")
	}

	return user, nil
}

// Create creates a new user
func (r *InMemoryAuthRepository) Create(user *domain.User) error {
	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.users[user.Username]; exists {
		return errors.New("user already exists")
	}

	r.users[user.Username] = user
	return nil
}
