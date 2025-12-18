package domain

import "time"

// User represents a user in the system
type User struct {
	ID        int       `json:"id" db:"id"`
	Username  string    `json:"username" db:"username"`
	Password  string    `json:"-" db:"password_hash"` // Never expose password in JSON
	Role      string    `json:"role" db:"role"`
	Name      string    `json:"name" db:"name"`
	CreatedAt time.Time `json:"created_at" db:"created_at"`
	UpdatedAt time.Time `json:"updated_at" db:"updated_at"`
}

// LoginRequest represents the login credentials
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse represents the authentication response
type LoginResponse struct {
	Token string   `json:"token"`
	User  UserInfo `json:"user"`
}

// UserInfo represents the public user information
type UserInfo struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Name     string `json:"name"`
}

// TokenClaims represents JWT token claims
type TokenClaims struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Exp      int64  `json:"exp"`
}

// TokenExpiry is the duration for token validity (24 hours)
const TokenExpiry = 24 * time.Hour
