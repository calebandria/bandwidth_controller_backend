package port

import "bandwidth_controller_backend/internal/core/domain"

// AuthService defines the interface for authentication operations
type AuthService interface {
	// Login authenticates a user and returns a token
	Login(username, password string) (*domain.LoginResponse, error)
	
	// ValidateToken validates a JWT token and returns the claims
	ValidateToken(token string) (*domain.TokenClaims, error)
	
	// GetUserByUsername retrieves a user by username
	GetUserByUsername(username string) (*domain.User, error)
}

// AuthRepository defines the interface for user data storage
type AuthRepository interface {
	// FindByUsername finds a user by username
	FindByUsername(username string) (*domain.User, error)
	
	// Create creates a new user (for future registration)
	Create(user *domain.User) error
}
