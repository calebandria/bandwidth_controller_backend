package service

import (
	"bandwidth_controller_backend/internal/core/domain"
	"bandwidth_controller_backend/internal/core/port"
	"context"
	"errors"
	"fmt"
	"log"
	"sync"
	"time"
	/* 	"github.com/go-co-op/gocron/v2" */)

const (
	ModeGlobalTBF       = "TBF"
	ModeGlobalHTB       = "HTB"
	DefaultIPRate       = "10mbit"        // Débit par défaut appliqué aux nouvelles connexions pour le tracking (conservateur pour éviter oversubscription)
	IPInactivityTimeout = 5 * time.Minute // Temps d'inactivité avant de retirer la limite QoS
)

// IPTrackingInfo stocke les informations nécessaires au suivi de l'état (y compris l'ID de la classe HTB)
type IPTrackingInfo struct {
	ClassID    uint16
	Rule       domain.QoSRule
	LastSeenAt time.Time // Timestamp de la dernière détection de l'IP
}

// IPRateMonitor définit les méthodes étendues que le driver doit supporter
// pour que le monitoring des statistiques par IP fonctionne.
// Ces méthodes sont celles que le service *suppose* être implémentées par le driver.

type QoSManager struct {
	driver           port.NetworkDriver
	statsStream      chan domain.TrafficUpdate // Canal pour envoyer les statistiques IP et globales
	threshold        uint64
	ilan             string
	inet             string
	activeMode       string
	globalLimit      string // Current global bandwidth limit (e.g., "100mbit")
	globalSetupMutex sync.Mutex

	// Suivi des IPs actuellement limitées (et donc avec une classe HTB).
	activeIPLimits map[string]IPTrackingInfo
	// Suivi de toutes les IPs détectées (même sans limite explicite)
	detectedIPs map[string]time.Time // IP -> LastSeenAt
	ipMutex     sync.RWMutex
}

func NewQoSManager(driver port.NetworkDriver, ilan string, inet string) *QoSManager {
	mgr := &QoSManager{
		driver:         driver,
		statsStream:    make(chan domain.TrafficUpdate, 100),
		threshold:      100000000,
		ilan:           ilan,
		inet:           inet,
		activeMode:     ModeGlobalTBF,
		globalLimit:    "0", // Will be set during SetupGlobalQoS
		activeIPLimits: make(map[string]IPTrackingInfo),
		detectedIPs:    make(map[string]time.Time),
	}
	return mgr
}

func (s *QoSManager) SetupGlobalQoS(ctx context.Context, ilan string, inet string, maxRate string) error {
	if err := s.driver.SetupHTBStructure(ctx, ilan, inet, maxRate); err != nil {
		return err
	}
	s.globalSetupMutex.Lock()
	s.globalLimit = maxRate
	s.globalSetupMutex.Unlock()
	s.activeMode = ModeGlobalHTB
	return nil
}

func (s *QoSManager) UpdateGlobalLimit(ctx context.Context, rule domain.QoSRule) error {
	// Si HTB n'est pas initialisé, l'initialiser automatiquement
	if !s.driver.IsHTBInitialized(ctx, rule.LanInterface, rule.WanInterface) {
		log.Println("WARNING: HTB not initialized. Initializing automatically with bandwidth:", rule.Bandwidth)
		s.globalSetupMutex.Lock()
		defer s.globalSetupMutex.Unlock()

		// Double-check après avoir acquis le lock
		if !s.driver.IsHTBInitialized(ctx, rule.LanInterface, rule.WanInterface) {
			if err := s.driver.SetupHTBStructure(ctx, rule.LanInterface, rule.WanInterface, rule.Bandwidth); err != nil {
				return fmt.Errorf("failed to auto-initialize HTB structure: %w", err)
			}
			s.activeMode = ModeGlobalHTB
			s.globalLimit = rule.Bandwidth // Store the limit
			log.Println("HTB structure auto-initialized successfully")
			// Pas besoin d'appeler ApplyGlobalShaping car SetupHTBStructure a déjà configuré la bande passante
			return nil
		}
	}

	if s.activeMode != ModeGlobalHTB {
		return errors.New("HTB mode is not active. Please call /qos/setup first")
	}

	// Update the stored global limit
	s.globalSetupMutex.Lock()
	s.globalLimit = rule.Bandwidth
	s.globalSetupMutex.Unlock()

	return s.driver.ApplyGlobalShaping(ctx, rule)
}

func (s *QoSManager) SetSimpleGlobalLimit(ctx context.Context, rule domain.QoSRule) error {
	if s.activeMode == ModeGlobalHTB {
		return errors.New("cannot apply TBF limit while HTB is active. Reset first")
	}
	s.activeMode = ModeGlobalTBF
	return s.driver.ApplyShaping(ctx, rule)
}

func (s *QoSManager) ResetQoS(ctx context.Context, ilan string, inet string) error {
	// Nettoyer toutes les limites IP actives avant de réinitialiser le qdisc root
	s.ipMutex.Lock()
	defer s.ipMutex.Unlock()

	tempIPs := make([]string, 0, len(s.activeIPLimits))
	for ip := range s.activeIPLimits {
		tempIPs = append(tempIPs, ip)
	}

	for _, ip := range tempIPs {
		rule := s.activeIPLimits[ip].Rule
		// On ignore l'erreur ici car le qdisc pourrait déjà être en cours de suppression
		_ = s.driver.RemoveIPRateLimit(ctx, ip, rule)
		delete(s.activeIPLimits, ip)
	}

	return s.driver.ResetShaping(ctx, ilan, inet)
}

func (s *QoSManager) GetConnectedLANIPs(ctx context.Context, ilan string) ([]string, error) {
	return s.driver.GetConnectedLANIPs(ctx, ilan)
}

// AddIPRateLimit applique une limite et met à jour l'état local du service.
func (s *QoSManager) AddIPRateLimit(ctx context.Context, ip string, rule domain.QoSRule) error {
	if s.activeMode != ModeGlobalHTB {
		return errors.New("HTB mode is not active. Cannot apply IP limits")
	}

	if !s.driver.IsHTBInitialized(ctx, rule.LanInterface, rule.WanInterface) {
		log.Println("WARNING: Base HTB structure missing. Attempting lazy setup now.")

		// Use a dedicated setup lock to prevent multiple goroutines from running SetupGlobalQoS simultaneously.
		// Assuming s.globalSetupMutex exists and protects SetupGlobalQoS from the outside.
		// If not, you must create one.
		s.globalSetupMutex.Lock() // Must lock BEFORE calling SetupGlobalQoS
		defer s.globalSetupMutex.Unlock()

		// Re-check after acquiring the lock (in case another goroutine just finished the setup)
		if !s.driver.IsHTBInitialized(ctx, rule.LanInterface, rule.WanInterface) {
			setupCtx, setupCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer setupCancel()

			// Re-run the critical setup logic
			if err := s.SetupGlobalQoS(setupCtx, rule.LanInterface, rule.WanInterface, rule.Bandwidth); err != nil {
				// If it fails here, the whole system has a problem, but we let it try again later.
				return fmt.Errorf("failed lazy setup of global HTB: %w", err)
			}
			log.Println("Lazy HTB setup successful. Proceeding with IP rule.")
		}
	}

	s.ipMutex.Lock()
	defer s.ipMutex.Unlock()

	// Vérifier si la règle existe déjà pour cette IP
	if info, ok := s.activeIPLimits[ip]; ok {
		// Supprimer l'ancienne classe pour en créer une nouvelle
		s.driver.RemoveIPRateLimit(ctx, ip, info.Rule)
		delete(s.activeIPLimits, ip)
		log.Printf("Existing rule for %s removed before re-adding.", ip)
	}

	// Le driver ajoute la règle (création de la classe HTB et du filtre)
	err := s.driver.AddIPRateLimit(ctx, ip, rule)
	if err != nil {
		log.Printf("Error adding IP rate limit for %s: %v", ip, err)
		return err
	}

	// Stockons juste la règle locale, en attendant que le moniteur récupère la ClassID via le driver.
	s.activeIPLimits[ip] = IPTrackingInfo{
		Rule: rule,
		// ClassID sera déduit/récupéré par le moniteur.
	}

	log.Printf("IP %s successfully added to rate limits.", ip)
	return nil
}

// RemoveIPRateLimit supprime une limite et met à jour l'état local du service.
func (s *QoSManager) RemoveIPRateLimit(ctx context.Context, ip string) error {
	s.ipMutex.Lock()
	defer s.ipMutex.Unlock()

	info, ok := s.activeIPLimits[ip]
	if !ok {
		return fmt.Errorf("IP %s not currently limited", ip)
	}

	if err := s.driver.RemoveIPRateLimit(ctx, ip, info.Rule); err != nil {
		log.Printf("Error removing IP rate limit for %s: %v", ip, err)
		// On log l'erreur mais on continue la suppression locale si elle est due à un état déjà propre du système.
	}

	delete(s.activeIPLimits, ip)
	log.Printf("IP %s successfully removed from rate limits.", ip)
	return nil
}

// GetStatsStream expose le canal pour la diffusion des statistiques en temps réel.
func (s *QoSManager) GetStatsStream() <-chan domain.TrafficUpdate {
	return s.statsStream
}

// StartIPMonitoring démarre une boucle de goroutine pour la détection et le suivi des IPs.
// La fonction de tracking doit être lancée après SetupGlobalQoS.
func (s *QoSManager) StartIPMonitoring(ctx context.Context, interval time.Duration) {
	log.Println("Starting IP monitoring loop...")

	// Map pour stocker les IPs détectées
	detectedIPs := make(map[string]bool)

	// Ticker pour la boucle de surveillance périodique
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Println("IP Monitoring stopped.")
			return
		case <-ticker.C:
			s.monitorLoop(ctx, detectedIPs)
		}
	}
}

// monitorLoop exécute les étapes de détection, gestion et calcul des statistiques.
// Correction : Suppression du Lock global ici car Add/RemoveIPRateLimit se verrouillent eux-mêmes.
func (s *QoSManager) monitorLoop(ctx context.Context, detectedIPs map[string]bool) {
	if s.activeMode != ModeGlobalHTB {
		return // Ne traquer les IPs que si HTB est actif
	}

	// 1. Détection des IPs actives via ARP/Neighbor table
	connectedIPs, err := s.driver.GetConnectedLANIPs(ctx, s.ilan)
	if err != nil {
		log.Printf("Error getting connected IPs: %v", err)
		return
	}

	currentConnectedSet := make(map[string]bool)
	for _, ip := range connectedIPs {
		currentConnectedSet[ip] = true
	}

	// =========================================================
	// 2. Gestion des Connexions/Déconnexions
	// =========================================================

	// --- Mettre à jour LastSeenAt pour les IPs connectées ---
	now := time.Now()
	s.ipMutex.Lock()
	for _, ip := range connectedIPs {
		if info, exists := s.activeIPLimits[ip]; exists {
			info.LastSeenAt = now
			s.activeIPLimits[ip] = info
		}
	}
	s.ipMutex.Unlock()

	// --- Identifier les IPs inactives depuis >IPInactivityTimeout ---
	ipsToRemove := make([]string, 0)
	s.ipMutex.RLock()
	for ip, info := range s.activeIPLimits {
		if !currentConnectedSet[ip] {
			// IP absente de la table ARP - vérifier le timeout
			if now.Sub(info.LastSeenAt) > IPInactivityTimeout {
				ipsToRemove = append(ipsToRemove, ip)
			}
		}
	}
	s.ipMutex.RUnlock()

	// --- Traiter les nouvelles IPs ---
	for _, ip := range connectedIPs {
		s.ipMutex.Lock()
		if _, exists := s.detectedIPs[ip]; !exists {
			// Nouvelle IP détectée - on la track sans créer de classe HTB
			log.Printf("New IP detected: %s. Tracking under global limit only (no per-IP class).", ip)

			// Créer des règles iptables de comptage pour pouvoir lire les stats
			if err := s.driver.EnsureIPCountingRules(ctx, ip, s.ilan, s.inet); err != nil {
				log.Printf("Warning: Failed to create counting rules for IP %s: %v", ip, err)
			}
		}
		s.detectedIPs[ip] = now // Mettre à jour LastSeenAt
		s.ipMutex.Unlock()
		detectedIPs[ip] = true // Marquer comme détecté lors de ce cycle
	}

	// --- Traiter les IPs déconnectées (inactives >5 min) ---
	for _, ip := range ipsToRemove {
		log.Printf("IP %s inactive for >%v. Removing rate limit.", ip, IPInactivityTimeout)

		// RemoveIPRateLimit gère son propre Lock()
		if err := s.RemoveIPRateLimit(ctx, ip); err != nil {
			log.Printf("Warning: Failed to remove rate limit for disconnected IP %s: %v", ip, err)
		}
	}

	// =========================================================
	// 3. Collecte et Calcul des Statistiques
	// =========================================================

	// Assertion de type pour accéder aux méthodes spécifiques au monitoring.
	driverMonitor, ok := s.driver.(port.IPRateMonitor)
	if !ok {
		log.Println("Driver does not support advanced IP tracking methods (missing IPRateMonitor interface).")
		return
	}

	// Construire la liste de toutes les IPs à envoyer au WebSocket
	allIPs := make(map[string]bool)

	s.ipMutex.RLock()
	// Ajouter les IPs détectées (sans limite)
	for ip := range s.detectedIPs {
		allIPs[ip] = true
	}
	// Ajouter les IPs avec limite explicite (ont une classe HTB)
	for ip := range s.activeIPLimits {
		allIPs[ip] = true
	}
	s.ipMutex.RUnlock()

	// Récupérer la map des IPs avec classe HTB active
	driverIPs := driverMonitor.GetActiveIPs()

	// Pour chaque IP détectée ou limitée, calculer et envoyer les stats
	for ip := range allIPs {
		var uploadRateMbps, downloadRateMbps float64
		isLimited := false
		bandwidthLimit := ""

		// Vérifier si cette IP a une classe HTB active (limite explicite)
		if classID, hasClass := driverIPs[ip]; hasClass {
			// IP avec classe HTB - lire les stats depuis HTB
			uploadStats, err := driverMonitor.GetInstantaneousClassStats(s.inet, classID)
			if err == nil {
				uploadRateMbps, _ = driverMonitor.CalculateIPRateMbps(ip, s.inet, classID, uploadStats)
			} else {
				log.Printf("Warning: Failed to read WAN stats for %s (1:%x): %v", ip, classID, err)
			}

			downloadStats, err := driverMonitor.GetInstantaneousClassStats(s.ilan, classID)
			if err == nil {
				downloadRateMbps, _ = driverMonitor.CalculateIPRateMbps(ip, s.ilan, classID, downloadStats)
			} else {
				log.Printf("Warning: Failed to read LAN stats for %s (1:%x): %v", ip, classID, err)
			}

			// Récupérer la limite configurée
			s.ipMutex.RLock()
			if info, ok := s.activeIPLimits[ip]; ok {
				isLimited = true
				bandwidthLimit = info.Rule.Bandwidth
			}
			s.ipMutex.RUnlock()
		} else {
			// IP détectée mais sans classe HTB - sous limite globale uniquement
			// Lire les stats depuis iptables counters
			uploadBytes, downloadBytes, err := driverMonitor.GetIPTrafficBytes(ctx, ip, s.inet)
			if err != nil {
				log.Printf("Warning: Failed to read iptables counters for %s: %v", ip, err)
				uploadRateMbps = 0.0
				downloadRateMbps = 0.0
			} else {
				// Calculer le taux en Mbps à partir des compteurs
				uploadRateMbps, _ = driverMonitor.CalculateIPTrafficRateFromIptables(ip, s.inet, uploadBytes, true)
				downloadRateMbps, _ = driverMonitor.CalculateIPTrafficRateFromIptables(ip, s.ilan, downloadBytes, false)
			}
			isLimited = false
			bandwidthLimit = "" // Vide = sous limite globale
		}

		ipStat := domain.IPTrafficStat{
			IP:             ip,
			UploadRate:     uploadRateMbps,
			DownloadRate:   downloadRateMbps,
			IsLimited:      isLimited,
			BandwidthLimit: bandwidthLimit,
			Status:         "Active",
		}

		// Envoi des statistiques IP au canal pour le consommateur WebSocket
		update := domain.TrafficUpdate{
			Type:   "ip",
			IPStat: &ipStat,
		}
		select {
		case s.statsStream <- update:
			// Sent successfully
		default:
			log.Println("Warning: Stats channel full, dropping IP data.")
		}
	}

	// =========================================================
	// 4. Calcul et Envoi des Statistiques Globales
	// =========================================================
	s.sendGlobalStats(ctx)
}

// sendGlobalStats calcule et envoie les statistiques globales des interfaces
func (s *QoSManager) sendGlobalStats(ctx context.Context) {
	// Vérifier si le contexte est annulé
	select {
	case <-ctx.Done():
		return
	default:
	}

	// Utiliser s.driver directement pour les méthodes globales
	globalDriver, ok := s.driver.(interface {
		GetInstantaneousNetDevStats(iface string) (domain.NetDevStats, error)
		CalculateRateMbps(iface string, currentStats domain.NetDevStats) (txRateMbps float64, rxRateMbps float64, err error)
	})

	if !ok {
		log.Println("Warning: Driver does not support global stats methods")
		return
	}

	// Statistiques LAN
	lanStats, err := globalDriver.GetInstantaneousNetDevStats(s.ilan)
	lanTxRate, lanRxRate := 0.0, 0.0
	if err == nil {
		lanTxRate, lanRxRate, _ = globalDriver.CalculateRateMbps(s.ilan, lanStats)
	}

	// Statistiques WAN
	wanStats, err := globalDriver.GetInstantaneousNetDevStats(s.inet)
	wanTxRate, wanRxRate := 0.0, 0.0
	if err == nil {
		wanTxRate, wanRxRate, _ = globalDriver.CalculateRateMbps(s.inet, wanStats)
	}

	// Compter les IPs actives et limitées
	s.ipMutex.RLock()
	totalActive := len(s.activeIPLimits)
	totalLimited := 0
	for _, info := range s.activeIPLimits {
		// Si la bande passante n'est pas la valeur par défaut, on la considère comme limitée
		if info.Rule.Bandwidth != DefaultIPRate {
			totalLimited++
		}
	}
	s.ipMutex.RUnlock()

	s.globalSetupMutex.Lock()
	currentGlobalLimit := s.globalLimit
	s.globalSetupMutex.Unlock()

	globalStat := domain.GlobalTrafficStat{
		LanInterface:    s.ilan,
		WanInterface:    s.inet,
		LanUploadRate:   lanTxRate,
		LanDownloadRate: lanRxRate,
		WanUploadRate:   wanTxRate,
		WanDownloadRate: wanRxRate,
		TotalActiveIPs:  totalActive,
		TotalLimitedIPs: totalLimited,
		GlobalLimit:     currentGlobalLimit,
		Timestamp:       time.Now(),
	}

	update := domain.TrafficUpdate{
		Type:       "global",
		GlobalStat: &globalStat,
	}

	// Envoi avec respect du contexte
	select {
	case <-ctx.Done():
		return
	case s.statsStream <- update:
		// Sent successfully
	default:
		log.Println("Warning: Stats channel full, dropping global data.")
	}
}

// BlockDevice bloque un appareil en utilisant iptables pour empêcher l'accès à Internet
func (s *QoSManager) BlockDevice(ctx context.Context, ip string) error {
	log.Printf("QoSManager: Blocking device %s", ip)
	return s.driver.BlockDevice(ctx, ip, s.inet)
}

// UnblockDevice débloque un appareil en supprimant les règles iptables
func (s *QoSManager) UnblockDevice(ctx context.Context, ip string) error {
	log.Printf("QoSManager: Unblocking device %s", ip)
	return s.driver.UnblockDevice(ctx, ip, s.inet)
}

// IsDeviceBlocked vérifie si un appareil est actuellement bloqué
func (s *QoSManager) IsDeviceBlocked(ctx context.Context, ip string) (bool, error) {
	return s.driver.IsDeviceBlocked(ctx, ip, s.inet)
}

// GetLanInterface returns the LAN interface name
func (s *QoSManager) GetLanInterface() string {
	return s.ilan
}

// GetWanInterface returns the WAN interface name
func (s *QoSManager) GetWanInterface() string {
	return s.inet
}

/* func() */
