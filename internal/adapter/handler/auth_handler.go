package handler

import (
	"bandwidth_controller_backend/internal/core/domain"
	"bandwidth_controller_backend/internal/core/port"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

type AuthHandler struct {
	authService port.AuthService
}

func NewAuthHandler(authService port.AuthService) *AuthHandler {
	return &AuthHandler{
		authService: authService,
	}
}

// Login
// @Summary User login
// @Description Authenticates a user with username and password, returns JWT token
// @Tags Authentication
// @Accept json
// @Produce json
// @Param credentials body domain.LoginRequest true "Login credentials"
// @Success 200 {object} domain.LoginResponse "token and user info"
// @Failure 400 {object} map[string]string "error: Invalid request format"
// @Failure 401 {object} map[string]string "error: Invalid credentials"
// @Router /auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req domain.LoginRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	// Call auth service
	response, err := h.authService.Login(req.Username, req.Password)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, response)
}

// Logout
// @Summary User logout
// @Description Logs out a user (client-side token removal, no server state)
// @Tags Authentication
// @Produce json
// @Success 200 {object} map[string]string "message: Logged out successfully"
// @Router /auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	// Since we're using stateless JWT, logout is handled on the client side
	// This endpoint is mainly for API consistency
	c.JSON(http.StatusOK, gin.H{"message": "Logged out successfully"})
}

// AuthMiddleware validates JWT token
func (h *AuthHandler) AuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get token from Authorization header
		authHeader := c.GetHeader("Authorization")
		if authHeader == "" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Authorization header required"})
			c.Abort()
			return
		}

		// Extract token from "Bearer <token>"
		parts := strings.Split(authHeader, " ")
		if len(parts) != 2 || parts[0] != "Bearer" {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid authorization format"})
			c.Abort()
			return
		}

		token := parts[1]

		// Validate token
		claims, err := h.authService.ValidateToken(token)
		if err != nil {
			c.JSON(http.StatusUnauthorized, gin.H{"error": "Invalid or expired token: " + err.Error()})
			c.Abort()
			return
		}

		// Store claims in context for use in handlers
		c.Set("username", claims.Username)
		c.Set("role", claims.Role)

		c.Next()
	}
}

// GetCurrentUser returns the authenticated user info
// @Summary Get current user
// @Description Returns information about the currently authenticated user
// @Tags Authentication
// @Produce json
// @Security BearerAuth
// @Success 200 {object} domain.UserInfo "user information"
// @Failure 401 {object} map[string]string "error: Unauthorized"
// @Router /auth/me [get]
func (h *AuthHandler) GetCurrentUser(c *gin.Context) {
	username, exists := c.Get("username")
	if !exists {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}

	user, err := h.authService.GetUserByUsername(username.(string))
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "User not found"})
		return
	}

	c.JSON(http.StatusOK, domain.UserInfo{
		Username: user.Username,
		Role:     user.Role,
		Name:     user.Name,
	})
}
