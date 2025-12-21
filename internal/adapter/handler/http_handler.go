package handler

import (
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/gorilla/websocket"

	"bandwidth_controller_backend/internal/core/domain"
	"bandwidth_controller_backend/internal/core/port"
	service "bandwidth_controller_backend/internal/core/service"
)

// --- Request Structs ---

type RuleUpdateRequest struct {
	RateLimit string `json:"rate_limit" binding:"required" example:"10mbit"`
	Latency   string `json:"latency" example:"50ms"`
}

type SetupRequest struct {
	TotalBandwidth string `json:"total_bandwidth" binding:"required" example:"10mbit"`
}

type IPControlRequest struct {
	IP        string `json:"ip" binding:"required" example:"192.168.1.10"`
	RateLimit string `json:"rate_limit,omitempty" example:"5mbit"`
}

type IPTrafficStat struct {
	IP           string  `json:"ip"`
	UploadRate   float64 `json:"upload_mbps"`
	DownloadRate float64 `json:"download_mbps"`
	IsLimited    bool    `json:"is_limited"`
	Status       string  `json:"status"` // e.g., "Active", "New", "Disconnected"

}

type InterfaceStats struct {
	InterfaceName     string                   `json:"interface_name" example:"eth0"`
	CurrentRateRxMbps float64                  `json:"current_rate_rx_mbps" example:"5.5"`
	CurrentRateTxMbps float64                  `json:"current_rate_tx_mbps" example:"12.8"`
	TotalBytesTx      uint64                   `json:"total_bytes_tx" example:"2351000"`
	TotalBytesRx      uint64                   `json:"total_bytes_rx" example:"1100000"`
	IPStats           map[string]IPTrafficStat `json:"ip_stats"` // Empty for now
}

// --- Handler Structure ---

type NetworkHandler struct {
	svc          port.QoSService
	netDriver    port.NetworkDriver
	upgrader     websocket.Upgrader
	lanInterface string
	wanInterface string
}

type ScheduleEntry struct {
	StartTime time.Time `json:"start_time" binding:"required" example:"2025-12-31T20:00:00Z"`
	EndTime   time.Time `json:"end_time" binding:"required" example:"2026-01-01T08:00:00Z"`
}

// @Description Requête pour planifier une mise à jour de la limite globale HTB.
type ScheduledRuleRequest struct {
	RuleUpdateRequest
	ScheduleEntry
}

type ScheduledIPControlRequest struct {
	IPControlRequest
	ScheduleEntry
}

func NewNetworkHandler(svc port.QoSService, netDriver port.NetworkDriver, ilan string, inet string) *NetworkHandler {
	return &NetworkHandler{
		svc:       svc,
		netDriver: netDriver,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		lanInterface: ilan,
		wanInterface: inet,
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

	if err := h.svc.SetupGlobalQoS(c.Request.Context(), h.lanInterface, h.wanInterface, req.TotalBandwidth); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to set up HTB structure: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "HTB structure successfully initialized", "lan_interface": h.lanInterface, "wan_interface": h.wanInterface, "rate": req.TotalBandwidth})
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
		LanInterface: h.lanInterface,
		WanInterface: h.wanInterface,
		Bandwidth:    req.RateLimit,
		Latency:      req.Latency,
		Enabled:      true,
	}

	if err := h.svc.UpdateGlobalLimit(c.Request.Context(), rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to update HTB limit: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{
		"status":        "HTB global limit updated successfully",
		"lan_interface": rule.LanInterface,
		"wan_interface": rule.WanInterface,
		"rate_limit":    rule.Bandwidth,
	})
}

// AddIPRateLimitHandler
// @Summary Applies a specific HTB rate limit to a client IP.
// @Description Adds a child class under the global HTB qdisc and applies a filter to match the specified IP address.
// @Tags HTB IP Control
// @Accept json
// @Produce json
// @Param request body IPControlRequest true "IP and Rate Limit Configuration"
// @Success 200 {object} map[string]interface{} "status: IP rate limit applied successfully"
// @Failure 400 {object} map[string]string "error: Invalid request format or missing field"
// @Failure 500 {object} map[string]string "error: Failed to apply IP rate limit"
// @Router /qos/ip/limit [post]
func (h *NetworkHandler) AddIPRateLimitHandler(c *gin.Context) {
	var req IPControlRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format or missing field: " + err.Error()})
		return
	}
	if req.RateLimit == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Rate limit is required for applying an IP limit."})
		return
	}

	rule := domain.QoSRule{
		LanInterface: h.lanInterface,
		WanInterface: h.wanInterface,
		Bandwidth:    req.RateLimit,
		Enabled:      true,
	}

	if err := h.svc.AddIPRateLimit(c.Request.Context(), req.IP, rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to apply rate limit for IP %s: %v", req.IP, err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "IP rate limit applied successfully", "ip": req.IP, "rate": req.RateLimit})
}

// RemoveIPRateLimitHandler
// @Summary Removes the HTB rate limit applied to a client IP.
// @Description Removes the specific child class and filter associated with the IP.
// @Tags HTB IP Control
// @Accept json
// @Produce json
// @Param request body IPControlRequest true "IP address to remove limit from"
// @Success 200 {object} map[string]interface{} "status: IP rate limit removed successfully"
// @Failure 400 {object} map[string]string "error: Invalid request format or missing field"
// @Failure 500 {object} map[string]string "error: Failed to remove IP rate limit"
// @Router /qos/ip/remove [post]
func (h *NetworkHandler) RemoveIPRateLimitHandler(c *gin.Context) {
	var req IPControlRequest

	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format or missing field: " + err.Error()})
		return
	}

	if err := h.svc.RemoveIPRateLimit(c.Request.Context(), req.IP); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("Failed to remove rate limit for IP %s: %v", req.IP, err)})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "IP rate limit removed successfully", "ip": req.IP})
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
		LanInterface: h.lanInterface,
		WanInterface: h.lanInterface,
		Bandwidth:    req.RateLimit,
		Latency:      req.Latency,
		Enabled:      true,
	}

	if err := h.svc.SetSimpleGlobalLimit(c.Request.Context(), rule); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to apply TBF rule: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"status": "Simple TBF rule applied successfully", "lan_interface": rule.LanInterface, "wan_interface": rule.WanInterface})
}

// ResetShapingHandler
// @Summary Removes all shaping (QDiscs) from both interfaces.
// @Description Deletes the root qdisc on both the specified LAN and WAN interfaces, effectively removing all HTB or TBF rules.
// @Tags Cleanup
// @Accept json
// @Produce json
// @Success 200 {object} map[string]interface{} "status: Shaping successfully reset on both interfaces"
// @Failure 400 {object} map[string]string "error: Invalid request format"
// @Failure 500 {object} map[string]string "error: Failed to reset shaping"
// @Router /qos/reset [post]
func (h *NetworkHandler) ResetShapingHandler(c *gin.Context) {

	if err := h.svc.ResetQoS(c.Request.Context(), h.lanInterface, h.wanInterface); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to reset shaping: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"status": "Shaping successfully reset on both interfaces"})
}

// GetConnectedLANIPsHandler
// @Summary Retrieves connected client IPs on the LAN interface.
// @Description Uses 'ip neighbor show' to find IPs in REACHABLE, STALE, or DELAY state on the local network interface.
// @Tags Status
// @Produce json
// @Success 200 {array} string "List of connected IPv4 or IPv6 (non-fe80) addresses"
// @Failure 400 {object} map[string]string "error: Missing or invalid lan_interface parameter"
// @Failure 500 {object} map[string]string "error: Failed to execute ip neighbor command"
// @Router /status/lanips [get]
func (h *NetworkHandler) GetConnectedLANIPsHandler(c *gin.Context) {
	lanInterface := h.lanInterface

	if lanInterface == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required query parameter: lan_interface"})
		return
	}
	ips, err := h.svc.GetConnectedLANIPs(c.Request.Context(), lanInterface)

	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to retrieve connected IPs: " + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"connected_ips": ips, "interface": lanInterface})
}

// GetTrafficStatsHandler (Unchanged from the user's input, still uses polling netDriver)
// @Summary Récupère le débit de trafic par interface en une seule fois (polling HTTP).
// @Description Lit les compteurs de /proc/net/dev et calcule le débit (Rx et Tx) en Mbps en utilisant l'état précédent stocké côté serveur.
// @Tags Monitoring
// @Produce json
// @Success 200 {object} InterfaceStats "Débit de trafic par interface"
// @Failure 400 {string} string "Requête invalide"
// @Failure 500 {string} string "Erreur lors de la récupération des statistiques"
// @Router /qos/stats [get]
func (h *NetworkHandler) GetTrafficStatsHandler(c *gin.Context) {
	lanInterface := h.lanInterface

	if lanInterface == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required query parameter: lan_interface"})
		return
	}

	// Interface LAN
	lanCurrentStats, err := h.netDriver.GetInstantaneousNetDevStats(lanInterface)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Erreur lors de la lecture des stats LAN", "details": err.Error()})
		return
	}
	lanTxRate, lanRxRate, _ := h.netDriver.CalculateRateMbps(lanInterface, lanCurrentStats)

	// Construction de la réponse
	response := InterfaceStats{
		InterfaceName:     lanInterface,
		CurrentRateTxMbps: lanTxRate,
		CurrentRateRxMbps: lanRxRate,
		TotalBytesTx:      lanCurrentStats.TxBytes,
		TotalBytesRx:      lanCurrentStats.RxBytes,
		IPStats:           make(map[string]IPTrafficStat),
	}
	c.JSON(http.StatusOK, response)
}

// StreamTrafficStatsHandler
// @Summary Ouvre une connexion WebSocket pour streamer le débit de trafic par IP en temps réel.
// @Description Met à niveau la connexion HTTP vers WebSocket et pousse les statistiques de débit par IP et globales (issues du QoSManager) toutes les ~2 secondes.
// @Tags Monitoring
// @Router /qos/stream [get]
func (h *NetworkHandler) StreamTrafficStatsHandler(c *gin.Context) {
	// Upgrade la connexion HTTP vers WebSocket
	conn, err := h.upgrader.Upgrade(c.Writer, c.Request, nil)
	if err != nil {
		log.Printf("Erreur d'upgrade WebSocket: %v", err)
		return
	}
	defer conn.Close()

	// Send initial snapshot immediately on connect
	qosManager, ok := h.svc.(interface {
		CurrentIPSnapshot() service.IPSnapshot
	})
	if ok {
		snapshot := qosManager.CurrentIPSnapshot()
		snapshotMsg := map[string]interface{}{
			"type":     "snapshot",
			"snapshot": snapshot,
		}
		if err := conn.WriteJSON(snapshotMsg); err != nil {
			log.Printf("Error sending initial snapshot: %v", err)
			return
		}
		log.Println("Initial snapshot sent to WebSocket client")
	}

	// Le service expose un canal de statistiques (IP et globales)
	statsStream := h.svc.GetStatsStream()

	log.Printf("Démarrage du streaming WebSocket pour statistiques IP et globales.")

	for {
		select {
		case <-c.Request.Context().Done():
			// Le client HTTP (Gin) est déconnecté/le contexte est annulé.
			log.Println("Client déconnecté (contexte HTTP terminé), arrêt du streaming.")
			return
		case update, ok := <-statsStream:
			if !ok {
				// Le canal du service a été fermé.
				log.Println("Le canal de statistiques du QoSManager est fermé.")
				return
			}

			// Envoyer l'update qui peut contenir soit des stats IP, soit des stats globales
			if err := conn.WriteJSON(update); err != nil {
				// Erreur d'écriture (client déconnecté, connexion rompue)
				log.Printf("Erreur d'écriture WebSocket (client déconnecté ?): %v", err)
				return
			}

			// Log optionnel pour debug
			// if update.Type == "ip" && update.IPStat != nil {
			// 	log.Printf("Streaming IP: %s (Tx: %.2f Mbps)", update.IPStat.IP, update.IPStat.UploadRate)
			// } else if update.Type == "global" && update.GlobalStat != nil {
			// 	log.Printf("Streaming Global: LAN Tx: %.2f Mbps", update.GlobalStat.LanUploadRate)
			// }
		default:
			// Utiliser un court délai pour éviter une boucle de CPU intensive si le canal est vide
			time.Sleep(10 * time.Millisecond)
		}
	}
}

// GetIPsSnapshotHandler
// @Summary Returns the current snapshot of all tracked IPs with sequence number
// @Description Returns a snapshot of IP state for persistence and synchronization. Includes sequence number and timestamp.
// @Tags Monitoring
// @Produce json
// @Success 200 {object} map[string]interface{} "snapshot with sequence, timestamp, and ips array"
// @Failure 500 {object} map[string]string "error: Failed to retrieve snapshot"
// @Router /qos/ips [get]
func (h *NetworkHandler) GetIPsSnapshotHandler(c *gin.Context) {
	qosManager, ok := h.svc.(interface {
		CurrentIPSnapshot() service.IPSnapshot
	})
	if !ok {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Snapshot not supported"})
		return
	}

	snapshot := qosManager.CurrentIPSnapshot()
	c.JSON(http.StatusOK, snapshot)
}

// ScheduleGlobalLimitHandler
// @Summary Planifie une limite de bande passante globale pour une période donnée.
// @Description Configure une limite HTB sur les interfaces spécifiées, active à l'heure de début et réinitialisée à l'heure de fin.
// @Tags Scheduling
// @Accept json
// @Produce json
// @Param rule body ScheduledRuleRequest true "Règle de QoS et Horaire"
// @Success 200 {object} gin.H{"status":"string"} "Planification de la règle globale réussie"
// @Failure 400 {string} string "Requête invalide"
// @Failure 500 {string} string "Erreur lors de la planification"
// @Router /qos/schedule/global [post]
func (h *NetworkHandler) ScheduleGlobalLimitHandler(c *gin.Context) {
	var req ScheduledRuleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	// Ici, vous appelleriez le service (h.svc) qui gère la logique de planification
	// (par exemple, enregistrer la tâche dans une base de données ou un planificateur local).
	log.Printf("Scheduling global limit %s from %s to %s on %s/%s",
		req.RateLimit, req.StartTime.Format(time.RFC3339), req.EndTime.Format(time.RFC3339), h.lanInterface, h.wanInterface)

	// TODO: Implémenter h.svc.ScheduleGlobalLimit(req)

	c.JSON(http.StatusOK, gin.H{"status": "Global limit scheduled successfully",
		"start_time": req.StartTime.Format(time.RFC3339), "end_time": req.EndTime.Format(time.RFC3339)})
}

// ScheduleIPLimitHandler
// @Summary Planifie une limite de bande passante pour une adresse IP spécifique pour une période donnée.
// @Description Configure une limite TBF (ou HTB) pour une adresse IP, active à l'heure de début et réinitialisée à l'heure de fin.
// @Tags Scheduling
// @Accept json
// @Produce json
// @Param rule body ScheduledIPControlRequest true "Règle de contrôle IP et Horaire"
// @Success 200 {object} gin.H{"status":"string"} "Planification de la règle IP réussie"
// @Failure 400 {string} string "Requête invalide"
// @Failure 500 {string} string "Erreur lors de la planification"
// @Router /qos/schedule/ip [post]
func (h *NetworkHandler) ScheduleIPLimitHandler(c *gin.Context) {
	var req ScheduledIPControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	// Ici, vous appelleriez le service (h.svc) qui gère la logique de planification
	// (par exemple, enregistrer la tâche dans une base de données ou un planificateur local).
	log.Printf("Scheduling IP limit %s for IP %s from %s to %s on %s",
		req.RateLimit, req.IP, req.StartTime.Format(time.RFC3339), req.EndTime.Format(time.RFC3339), h.lanInterface)

	// TODO: Implémenter h.svc.ScheduleIPLimit(req)

	c.JSON(http.StatusOK, gin.H{"status": "IP limit scheduled successfully",
		"ip_address": req.IP, "start_time": req.StartTime.Format(time.RFC3339), "end_time": req.EndTime.Format(time.RFC3339)})
}

// BlockDeviceHandler
// @Summary Bloque un appareil en empêchant son accès à Internet
// @Description Ajoute des règles iptables DROP pour bloquer le trafic d'une IP vers/depuis le WAN
// @Tags Device Control
// @Accept json
// @Produce json
// @Param request body IPControlRequest true "IP de l'appareil à bloquer"
// @Success 200 {object} map[string]interface{} "status: Device blocked successfully"
// @Failure 400 {object} map[string]string "error: Invalid request format"
// @Failure 500 {object} map[string]string "error: Failed to block device"
// @Router /qos/device/block [post]
func (h *NetworkHandler) BlockDeviceHandler(c *gin.Context) {
	var req IPControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	if err := h.svc.BlockDevice(c.Request.Context(), req.IP); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to block device: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "Device blocked successfully",
		"ip":     req.IP,
	})
}

// UnblockDeviceHandler
// @Summary Débloque un appareil en restaurant son accès à Internet
// @Description Supprime les règles iptables DROP pour débloquer le trafic d'une IP
// @Tags Device Control
// @Accept json
// @Produce json
// @Param request body IPControlRequest true "IP de l'appareil à débloquer"
// @Success 200 {object} map[string]interface{} "status: Device unblocked successfully"
// @Failure 400 {object} map[string]string "error: Invalid request format"
// @Failure 500 {object} map[string]string "error: Failed to unblock device"
// @Router /qos/device/unblock [post]
func (h *NetworkHandler) UnblockDeviceHandler(c *gin.Context) {
	var req IPControlRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format: " + err.Error()})
		return
	}

	if err := h.svc.UnblockDevice(c.Request.Context(), req.IP); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to unblock device: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"status": "Device unblocked successfully",
		"ip":     req.IP,
	})
}

// GetDeviceStatusHandler
// @Summary Vérifie si un appareil est bloqué
// @Description Retourne le statut de blocage d'une IP
// @Tags Device Control
// @Accept json
// @Produce json
// @Param ip query string true "IP de l'appareil à vérifier"
// @Success 200 {object} map[string]interface{} "ip, blocked"
// @Failure 400 {object} map[string]string "error: IP parameter required"
// @Failure 500 {object} map[string]string "error: Failed to check device status"
// @Router /qos/device/status [get]
func (h *NetworkHandler) GetDeviceStatusHandler(c *gin.Context) {
	ip := c.Query("ip")
	if ip == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "IP parameter required"})
		return
	}

	blocked, err := h.svc.IsDeviceBlocked(c.Request.Context(), ip)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Failed to check device status: " + err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"ip":      ip,
		"blocked": blocked,
	})
}
