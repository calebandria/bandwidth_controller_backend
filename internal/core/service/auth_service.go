package service

import (
	"bandwidth_controller_backend/internal/core/domain"
	"bandwidth_controller_backend/internal/core/port"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

type AuthServiceImpl struct {
	repo port.AuthRepository
}

func NewAuthService(repo port.AuthRepository) port.AuthService {
	return &AuthServiceImpl{
		repo: repo,
	}
}

// Login authenticates a user and generates a JWT token
func (s *AuthServiceImpl) Login(username, password string) (*domain.LoginResponse, error) {
	// Validate input
	if username == "" || password == "" {
		return nil, errors.New("username and password are required")
	}

	// Find user in repository
	user, err := s.repo.FindByUsername(username)
	if err != nil {
		return nil, errors.New("invalid credentials")
	}

	// Verify password (in production, use bcrypt.CompareHashAndPassword)
	if user.Password != password {
		return nil, errors.New("invalid credentials")
	}

	// Generate token
	token, err := s.generateToken(user)
	if err != nil {
		return nil, fmt.Errorf("failed to generate token: %w", err)
	}

	return &domain.LoginResponse{
		Token: token,
		User: domain.UserInfo{
			Username: user.Username,
			Role:     user.Role,
			Name:     user.Name,
		},
	}, nil
}

// ValidateToken validates a JWT token and extracts claims
func (s *AuthServiceImpl) ValidateToken(token string) (*domain.TokenClaims, error) {
	// Decode the base64 token
	decoded, err := base64.StdEncoding.DecodeString(token)
	if err != nil {
		return nil, errors.New("invalid token format")
	}

	// Parse the JSON claims
	var claims domain.TokenClaims
	if err := json.Unmarshal(decoded, &claims); err != nil {
		return nil, errors.New("invalid token claims")
	}

	// Check expiration
	if time.Now().Unix() > claims.Exp {
		return nil, errors.New("token expired")
	}

	return &claims, nil
}

// GetUserByUsername retrieves a user by username
func (s *AuthServiceImpl) GetUserByUsername(username string) (*domain.User, error) {
	return s.repo.FindByUsername(username)
}

// generateToken creates a base64-encoded JWT-like token
func (s *AuthServiceImpl) generateToken(user *domain.User) (string, error) {
	claims := domain.TokenClaims{
		Username: user.Username,
		Role:     user.Role,
		Exp:      time.Now().Add(domain.TokenExpiry).Unix(),
	}

	// Marshal claims to JSON
	claimsJSON, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}

	// Encode to base64 (simulating JWT for simplicity)
	// In production, use a proper JWT library like github.com/golang-jwt/jwt
	token := base64.StdEncoding.EncodeToString(claimsJSON)
	return token, nil
}
