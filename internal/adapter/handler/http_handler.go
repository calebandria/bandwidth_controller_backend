package handler

import (
	"log"
	"net/http"
	"time"

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
	LanInterface   string `json:"lan_interface" binding:"required" example:"eth0"`
	WanInterface   string `json:"wan_interface" binding:"required" example:"wlan1"`
	TotalBandwidth string `json:"total_bandwidth" binding:"required" example:"10mbit"`
}

type ResetRequest struct {
	LanInterface string `json:"lan_interface" binding:"required" example:"eth0"`
	WanInterface string `json:"wan_interface" binding:"required" example:"wlan1"`
}

type NetworkHandler struct {
	svc          port.QoSService
	netDriver    port.NetworkDriver
	upgrader     websocket.Upgrader
	lanInterface string
}

type LanInterfaceRequest struct {
	LanInterface string `json:"lan_interface" form:"lan_interface" binding:"required" example:"eth0"`
}

type IPTrafficStat struct {
    Bytes uint64 `json:"bytes" example:"1234567"`
    Packets uint64 `json:"packets" example:"1500"`
}

type InterfaceStats struct {
	InterfaceName string `json:"interface_name" example:"eth0"`
	CurrentRateRxMbps float64 `json:"current_rate_rx_mbps" example:"5.5"` 
	CurrentRateTxMbps float64 `json:"current_rate_tx_mbps" example:"12.8"` 
	TotalBytesTx  uint64 `json:"total_bytes_tx" example:"2351000"` 
	TotalBytesRx  uint64 `json:"total_bytes_rx" example:"1100000"` 
    
	IPStats  map[string]IPTrafficStat `json:"ip_stats"` // Empty for now
}

func NewNetworkHandler(svc port.QoSService, netDriver port.NetworkDriver, iface string) *NetworkHandler {
	return &NetworkHandler{
		svc:       svc,
		netDriver: netDriver,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool { return true },
		},
		lanInterface: iface,
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

// GetConnectedLANIPsHandler
// @Summary Retrieves connected client IPs on the LAN interface.
// @Description Uses 'ip neighbor show' to find IPs in REACHABLE, STALE, or DELAY state on the local network interface.
// @Tags Status
// @Produce json
// @Param lan_interface query string true "The LAN interface name (e.g., eth0)"
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

// GetTrafficStatsHandler (Updated to use the new rate calculation logic for consistency, but still polling)
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
// @Summary Ouvre une connexion WebSocket pour streamer le débit de trafic en temps réel.
// @Description Met à niveau la connexion HTTP vers WebSocket et pousse les statistiques de débit toutes les 1 seconde.
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
	
	// Récupérer les interfaces (dans un cas réel, ces valeurs devraient être passées via la requête ou la configuration)
	// Pour cette démo, nous utilisons des valeurs par défaut.
	lanInterface := h.lanInterface

	if lanInterface == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Missing required query parameter: lan_interface"})
		return
	}

	log.Printf("Démarrage du streaming WebSocket pour %s (intervalle 1s)", lanInterface)

	// Boucle de streaming en temps réel
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()
	
	for {
		select {
		case <-c.Request.Context().Done():
			log.Println("Client déconnecté (contexte HTTP terminé), arrêt du streaming.")
			return
		case <-ticker.C:
			// 1. Lire les stats et calculer le débit
			lanCurrentStats, errLan := h.netDriver.GetInstantaneousNetDevStats(lanInterface)

			if errLan != nil {
				log.Printf("Erreur lors de la lecture des stats: LAN=%v", errLan)
				continue 
			}

			lanTxRate, lanRxRate, _ := h.netDriver.CalculateRateMbps(lanInterface, lanCurrentStats)
			
			// 2. Construire le paquet de données
			data :=  InterfaceStats{
					InterfaceName: lanInterface,
					CurrentRateTxMbps: lanTxRate,
					CurrentRateRxMbps: lanRxRate,
					TotalBytesTx: lanCurrentStats.TxBytes,
					TotalBytesRx: lanCurrentStats.RxBytes,
					IPStats: make(map[string]IPTrafficStat), 
				}
			if err := conn.WriteJSON(data); err != nil {
				log.Printf("Erreur d'écriture WebSocket (client déconnecté ?): %v", err)
				return 
			}

			log.Printf("Streaming: Envoi des données (LAN Tx: %.2f Mbps)", lanTxRate)
		}
	}
}


