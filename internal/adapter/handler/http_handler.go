package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"bandwidth_controller_backend/internal/core/domain"
	"bandwidth_controller_backend/internal/core/port"
)

// --- Request Structs ---

type RuleUpdateRequest struct {
	LanInterface string `json:"lan_interface" binding:"required" example:"eth0"`
	WanInterface string `json:"wan_interface" binding:"required" example:"wlan1"` 
	RateLimit    string `json:"rate_limit" binding:"required" example:"10mbit"`
	Latency      string `json:"latency" example:"50ms"`
}

type SetupRequest struct {
	LanInterface string `json:"lan_interface" binding:"required" example:"eth0"`
	WanInterface   string `json:"wan_interface" binding:"required" example:"wlan1"`
	TotalBandwidth string `json:"total_bandwidth" binding:"required" example:"10mbit"`
}

type ResetRequest struct {
	LanInterface string `json:"lan_interface" binding:"required" example:"eth0"`
	WanInterface   string `json:"wan_interface" binding:"required" example:"wlan1"`
}


type NetworkHandler struct {
	svc      port.QoSService
	upgrader websocket.Upgrader
}

func NewNetworkHandler(svc port.QoSService) *NetworkHandler {
	return &NetworkHandler{
		svc: svc,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
	}
}

// --- Handlers ---

// SetupGlobalHandler
// @Summary Initializes the HTB (Hierarchical Token Bucket) structure.
// @Description Sets up the root HTB qdisc and class 1:1 on both LAN and WAN interfaces.
// @Tags HTB Setup
// @Accept json
// @Produce json
// @Param request body SetupRequest true "Interface and Global Bandwidth Configuration"
// @Success 200 {object} map[string]interface{} "status: HTB structure successfully initialized"
// @Failure 400 {object} map[string]string "error: Invalid request format"
// @Failure 500 {object} map[string]string "error: Failed to set up HTB structure"
// @Router /qos/setup [post]
func (h *NetworkHandler) SetupGlobalHandler(c *gin.Context) {
	var req SetupRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}
	
	if err := h.svc.SetupGlobalQoS(c.Request.Context(), req.LanInterface, req.WanInterface, req.TotalBandwidth); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set up HTB structure: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "HTB structure successfully initialized", "lan_interface": req.LanInterface, "wan_interface": req.WanInterface, "rate": req.TotalBandwidth})
}

// UpdateHTBGlobalLimit
// @Summary Updates the global rate limit for the existing HTB structure.
// @Description Changes the rate/ceil of the HTB root class (1:1) on both LAN and WAN interfaces.
// @Tags HTB Setup
// @Accept json
// @Produce json
// @Param request body RuleUpdateRequest true "Interface and Bandwidth Update"
// @Success 200 {object} map[string]interface{} "status: HTB global limit updated successfully"
// @Failure 400 {object} map[string]string "error: Invalid request format"
// @Failure 500 {object} map[string]string "error: Failed to update HTB limit"
// @Router /qos/htb/global/limit [post]
func (h *NetworkHandler) UpdateHTBGlobalLimit(c *gin.Context) {
	var req RuleUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	rule := domain.QoSRule{
		LanInterface: req.LanInterface,
		WanInterface: req.WanInterface, 
		Bandwidth:    req.RateLimit,
		Latency:      req.Latency,
		Enabled:      true,
	}

	if err := h.svc.UpdateGlobalLimit(c.Request.Context(), rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update HTB limit: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "HTB global limit updated successfully", "lan_interface": req.LanInterface, "wan_interface": req.WanInterface})
}

// UpdateSimpleLimit
// @Summary Applies a simple interface-wide limit using TBF.
// @Description Applies a simple Token Bucket Filter (TBF) qdisc to the root of both LAN and WAN interfaces.
// @Tags Simple TBF
// @Accept json
// @Produce json
// @Param request body RuleUpdateRequest true "Interface and Bandwidth/Latency parameters"
// @Success 200 {object} map[string]interface{} "status: Simple TBF rule applied successfully"
// @Failure 400 {object} map[string]string "error: Invalid request format or missing field"
// @Failure 500 {object} map[string]string "error: Failed to apply TBF rule"
// @Router /qos/simple/limit [post]
func (h *NetworkHandler) UpdateSimpleLimit(c *gin.Context) {
	var req RuleUpdateRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format or missing field: " + err.Error()})
		return
	}

	rule := domain.QoSRule{
		LanInterface: req.LanInterface,
		WanInterface: req.WanInterface,
		Bandwidth:    req.RateLimit,
		Latency:      req.Latency,
		Enabled:      true,
	}


	if err := h.svc.SetSimpleGlobalLimit(c.Request.Context(), rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to apply TBF rule: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "Simple TBF rule applied successfully", "lan_interface": req.LanInterface, "wan_interface": req.WanInterface})
}

// ResetShapingHandler
// @Summary Removes all shaping (QDiscs) from both interfaces.
// @Description Deletes the root qdisc on both the specified LAN and WAN interfaces, effectively removing all HTB or TBF rules.
// @Tags Cleanup
// @Accept json
// @Produce json
// @Param request body SetupRequest true "Interfaces to reset"
// @Success 200 {object} map[string]interface{} "status: Shaping successfully reset on both interfaces"
// @Failure 400 {object} map[string]string "error: Invalid request format"
// @Failure 500 {object} map[string]string "error: Failed to reset shaping"
// @Router /qos/reset [post]
func (h *NetworkHandler) ResetShapingHandler(c *gin.Context) {
    var req ResetRequest 
    
    if err := c.ShouldBindJSON(&req); err != nil {
        c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format, need lan_interface and wan_interface: " + err.Error()})
        return
    }

    if err := h.svc.ResetQoS(c.Request.Context(), req.LanInterface, req.WanInterface); err != nil {
        c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset shaping: " + err.Error()})
        return
    }
    c.JSON(http.StatusOK, gin.H{"status": "Shaping successfully reset on both interfaces"})
}