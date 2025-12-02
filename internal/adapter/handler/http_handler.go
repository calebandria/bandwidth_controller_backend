package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"bandwidth_controller_backend/internal/core/domain"
	"bandwidth_controller_backend/internal/core/port"
)

// RuleUpdateRequest defines the expected JSON input for the limit APIs.
type RuleUpdateRequest struct {
	Interface string `json:"interface" binding:"required"`
	RateLimit string `json:"rate_limit" binding:"required"` // Maps to domain.QoSRule.Bandwidth
	Latency   string `json:"latency"`
}

// NetworkHandler holds the QoS Service and the WebSocket configuration.
type NetworkHandler struct {
	svc      port.QoSService
	upgrader websocket.Upgrader
}

// SetupRequest defines the expected JSON input for the /qos/setup API.
type SetupRequest struct {
	Interface      string `json:"interface" binding:"required"`
	TotalBandwidth string `json:"total_bandwidth" binding:"required"`
}

// NewNetworkHandler constructs a new handler instance.
func NewNetworkHandler(svc port.QoSService) *NetworkHandler {
	return &NetworkHandler{
		svc: svc,
		upgrader: websocket.Upgrader{
			// WARNING: CheckOrigin is set to true for development only.
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// ---------------------------------------------------------------------
// HTB FEATURE ENDPOINTS (Control/Write Operations)
// ---------------------------------------------------------------------

// SetupGlobalHandler handles the POST request to initialize the HTB structure.
// Endpoint: POST /qos/setup
func (h *NetworkHandler) SetupGlobalHandler(c *gin.Context) {
	var req SetupRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	// Call the Service to set up the HTB structure and switch mode
	if err := h.svc.SetupGlobalQoS(c.Request.Context(), req.Interface, req.TotalBandwidth); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set up HTB structure: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "HTB structure successfully initialized", "rate": req.TotalBandwidth})
}

// UpdateHTBGlobalLimit handles POST /qos/htb/global/limit (Updates the HTB 1:1 root class)
func (h *NetworkHandler) UpdateHTBGlobalLimit(c *gin.Context) {
	var req RuleUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	rule := domain.QoSRule{
		Interface: req.Interface,
		Bandwidth: req.RateLimit, // Map RateLimit to Bandwidth
		Latency:   req.Latency,
		Enabled:   true,
	}

	// Call the Service method specific to updating the HTB root limit
	if err := h.svc.UpdateGlobalLimit(c.Request.Context(), rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update HTB limit: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "HTB global limit updated successfully", "interface": req.Interface})
}

// ---------------------------------------------------------------------
// SIMPLE TBF ENDPOINT (Control/Write Operation)
// ---------------------------------------------------------------------

// UpdateSimpleLimit handles the POST request to set a simple TBF limit.
// Endpoint: POST /qos/simple/limit
func (h *NetworkHandler) UpdateSimpleLimit(c *gin.Context) {
	var req RuleUpdateRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format or missing field: " + err.Error()})
		return
	}

	rule := domain.QoSRule{
		Interface: req.Interface,
		Bandwidth: req.RateLimit, // Map RateLimit to Bandwidth
		Latency:   req.Latency,
		Enabled:   true,
	}

	// Call the Service logic for the simple TBF mode
	if err := h.svc.SetSimpleGlobalLimit(c.Request.Context(), rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to apply TBF rule: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "Simple TBF rule applied successfully", "interface": req.Interface})
}

// ---------------------------------------------------------------------
// WebSocket Endpoint: GET /ws/stats (Monitoring/Read Operation)
// ---------------------------------------------------------------------

// StreamStats handles the WebSocket upgrade and streams live data from the Service.
func (h *NetworkHandler) StreamStats(c *gin.Context) {
	ws, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		return
	}
	defer ws.Close()

	statsChan := h.svc.GetLiveStats()

	// Loop forever, pushing data from the Service to the client
	for stat := range statsChan {
		if err := ws.WriteJSON(stat); err != nil {
			// If writing fails, the client disconnected, so break the loop
			break
		}
	}
}