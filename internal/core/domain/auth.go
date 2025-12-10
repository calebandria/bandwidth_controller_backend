package domain

import "time"

// User represents a user in the system
type User struct {
	Username string `json:"username"`
	Password string `json:"-"` // Never expose password in JSON
	Role     string `json:"role"`
	Name     string `json:"name"`
}

// LoginRequest represents the login credentials
type LoginRequest struct {
	Username string `json:"username" binding:"required"`
	Password string `json:"password" binding:"required"`
}

// LoginResponse represents the authentication response
type LoginResponse struct {
	Token string `json:"token"`
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
	Username string `json:"sub"`
	Role     string `json:"role"`
	Exp      int64  `json:"exp"`
}

// TokenExpiry is the duration for token validity (24 hours)
const TokenExpiry = 24 * time.Hour
