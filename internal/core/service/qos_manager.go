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
	driver             port.NetworkDriver
	deviceRepo         port.DeviceRepository
	trafficHistoryRepo port.TrafficHistoryRepository
	statsStream        chan domain.TrafficUpdate // Canal pour envoyer les statistiques IP et globales
	threshold          uint64
	ilan               string
	inet               string
	activeMode         string
	globalLimit        string // Current global bandwidth limit (e.g., "100mbit")
	globalSetupMutex   sync.Mutex

	// Suivi des IPs actuellement limitées (et donc avec une classe HTB).
	activeIPLimits map[string]IPTrackingInfo
	// Suivi de toutes les IPs détectées (même sans limite explicite)
	detectedIPs map[string]time.Time // IP -> LastSeenAt
	// Track last known MAC for each IP to detect MAC changes
	lastKnownMAC map[string]string // IP -> MAC
	ipMutex      sync.RWMutex

	// Sequence counter for state synchronization
	stateSequence uint64
	sequenceMutex sync.Mutex
}

func NewQoSManager(driver port.NetworkDriver, deviceRepo port.DeviceRepository, trafficHistoryRepo port.TrafficHistoryRepository, ilan string, inet string) *QoSManager {
	mgr := &QoSManager{
		driver:             driver,
		deviceRepo:         deviceRepo,
		trafficHistoryRepo: trafficHistoryRepo,
		statsStream:        make(chan domain.TrafficUpdate, 100),
		threshold:          100000000,
		ilan:               ilan,
		inet:               inet,
		activeMode:         ModeGlobalTBF,
		globalLimit:        "0", // Will be set during SetupGlobalQoS
		activeIPLimits:     make(map[string]IPTrackingInfo),
		detectedIPs:        make(map[string]time.Time),
		lastKnownMAC:       make(map[string]string),
		stateSequence:      0,
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
	// Note: We unlock manually before sending to channel to avoid deadlock

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

	// Increment sequence on state change
	s.incrementSequence()

	// Get MAC address while we still have the lock
	mac := s.lastKnownMAC[ip]

	// Release the lock BEFORE sending to channel to avoid deadlock
	s.ipMutex.Unlock()

	// Immediately broadcast the change to WebSocket clients
	ipStat := domain.IPTrafficStat{
		IP:             ip,
		MACAddress:     mac,
		UploadRate:     0,
		DownloadRate:   0,
		IsLimited:      true,
		BandwidthLimit: rule.Bandwidth,
		Status:         "Active",
	}
	update := domain.TrafficUpdate{
		Type:   "ip",
		IPStat: &ipStat,
	}

	// Try to send update without blocking
	select {
	case s.statsStream <- update:
		log.Printf("Broadcasted IP limit update for %s via WebSocket", ip)
	default:
		log.Printf("Warning: Stats channel full, IP limit update for %s will be sent on next monitoring cycle", ip)
	}

	// Persist to database (async to avoid blocking the HTTP response)
	if s.deviceRepo != nil {
		go func() {
			bgCtx := context.Background()
			if err := s.deviceRepo.UpdateLimit(bgCtx, ip, &rule.Bandwidth); err != nil {
				log.Printf("Failed to persist IP limit to database for %s: %v", ip, err)
			} else {
				log.Printf("IP limit for %s persisted to database: %s", ip, rule.Bandwidth)
			}
		}()
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

	// Increment sequence on state change
	s.incrementSequence()

	// Immediately broadcast the change to WebSocket clients
	s.ipMutex.RLock()
	mac := s.lastKnownMAC[ip]
	s.ipMutex.RUnlock()

	ipStat := domain.IPTrafficStat{
		IP:             ip,
		MACAddress:     mac,
		UploadRate:     0,
		DownloadRate:   0,
		IsLimited:      false,
		BandwidthLimit: "", // Empty = global limit
		Status:         "Active",
	}
	update := domain.TrafficUpdate{
		Type:   "ip",
		IPStat: &ipStat,
	}
	select {
	case s.statsStream <- update:
		// Sent successfully
	default:
		// Channel full, will be updated on next monitoring cycle
	}

	// Clear the limit in database (async) - set to NULL
	if s.deviceRepo != nil {
		go func() {
			bgCtx := context.Background()
			if err := s.deviceRepo.UpdateLimit(bgCtx, ip, nil); err != nil {
				log.Printf("Failed to clear limit in database for %s: %v", ip, err)
			} else {
				log.Printf("Limit cleared in database for %s", ip)
			}
		}()
	}

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

	// --- Traiter les nouvelles IPs et vérifier les changements de MAC ---
	for _, ip := range connectedIPs {
		// Get current MAC address
		var currentMAC string
		if macDriver, ok := s.driver.(interface {
			GetMACFromIP(context.Context, string) (string, error)
		}); ok {
			if mac, err := macDriver.GetMACFromIP(ctx, ip); err == nil {
				currentMAC = mac
			}
		}

		s.ipMutex.Lock()
		isNewIP := false
		macChanged := false

		if _, exists := s.detectedIPs[ip]; !exists {
			// Nouvelle IP détectée
			log.Printf("New IP detected: %s. Tracking under global limit only (no per-IP class).", ip)
			isNewIP = true

			// Créer des règles iptables de comptage pour pouvoir lire les stats
			if err := s.driver.EnsureIPCountingRules(ctx, ip, s.ilan, s.inet); err != nil {
				log.Printf("Warning: Failed to create counting rules for IP %s: %v", ip, err)
			}
		} else if currentMAC != "" {
			// IP exists - check if MAC changed
			if lastMAC, exists := s.lastKnownMAC[ip]; exists && lastMAC != currentMAC {
				log.Printf("MAC change detected for IP %s: %s -> %s", ip, lastMAC, currentMAC)
				macChanged = true

				// Check if the old device was blocked
				var wasBlocked bool
				if s.deviceRepo != nil {
					checkCtx := context.Background()
					oldDevice, err := s.deviceRepo.FindByIP(checkCtx, ip)
					if err == nil && oldDevice != nil && oldDevice.IsBlocked {
						wasBlocked = true
						log.Printf("Old device at %s (MAC: %s) was blocked, unblocking due to MAC change", ip, lastMAC)

						// Unblock the IP since it's a different device now
						if err := s.driver.UnblockDevice(checkCtx, ip, s.inet); err != nil {
							log.Printf("Warning: Failed to unblock %s after MAC change: %v", ip, err)
						}
					}
				}

				// Delete old device record from database (different device now)
				if s.deviceRepo != nil {
					go func(oldIP, oldMAC string, blocked bool) {
						bgCtx := context.Background()
						// Delete by IP since the old MAC+IP combination no longer exists
						if err := s.deviceRepo.Delete(bgCtx, oldIP); err != nil {
							log.Printf("Failed to delete old device record for %s (MAC: %s): %v", oldIP, oldMAC, err)
						} else {
							if blocked {
								log.Printf("Deleted old blocked device record for %s (MAC: %s)", oldIP, oldMAC)
							} else {
								log.Printf("Deleted old device record for %s (MAC: %s)", oldIP, oldMAC)
							}
						}
					}(ip, lastMAC, wasBlocked)
				}

				// If this IP had a custom limit, remove it since it's a different device
				if info, hasLimit := s.activeIPLimits[ip]; hasLimit {
					log.Printf("Removing custom limit for IP %s due to MAC address change", ip)

					// Remove the tc rule while we have the lock
					if err := s.driver.RemoveIPRateLimit(ctx, ip, info.Rule); err != nil {
						log.Printf("Warning: Failed to remove tc rule after MAC change for %s: %v", ip, err)
					}

					// Remove from active limits
					delete(s.activeIPLimits, ip)

					// Increment sequence
					s.incrementSequence()

					// Clear limit in database (async)
					if s.deviceRepo != nil {
						go func(oldIP string) {
							bgCtx := context.Background()
							if err := s.deviceRepo.UpdateLimit(bgCtx, oldIP, nil); err != nil {
								log.Printf("Failed to clear limit in database for %s: %v", oldIP, err)
							} else {
								log.Printf("Limit cleared in database for %s", oldIP)
							}
						}(ip)
					}

					log.Printf("IP %s limit removed due to MAC change", ip)
				}
			}
		}

		s.detectedIPs[ip] = now // Mettre à jour LastSeenAt
		if currentMAC != "" {
			s.lastKnownMAC[ip] = currentMAC // Track current MAC
		}
		s.ipMutex.Unlock()

		// Update database for new IPs or MAC changes
		if (isNewIP || macChanged) && s.deviceRepo != nil && currentMAC != "" {
			go func(ip, mac string) {
				bgCtx := context.Background()
				device := &domain.Device{
					IP:         ip,
					MACAddress: mac,
					// No limit initially - will be set when user applies one
				}
				if err := s.deviceRepo.Upsert(bgCtx, device); err != nil {
					log.Printf("Failed to upsert device %s (MAC: %s) to database: %v", ip, mac, err)
				} else {
					log.Printf("Device %s (MAC: %s) saved to database", ip, mac)
				}
			}(ip, currentMAC)
		}

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

	// Collect all IP stats for history saving
	var allIPStats []domain.IPTrafficStat

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

		// Get MAC address and hostname for this IP
		var macAddress, hostname string
		s.ipMutex.RLock()
		macAddress = s.lastKnownMAC[ip]
		s.ipMutex.RUnlock()

		// Try to get hostname from reverse DNS (non-blocking)
		if macDriver, ok := s.driver.(interface {
			GetHostnameFromIP(context.Context, string) (string, error)
		}); ok {
			if host, err := macDriver.GetHostnameFromIP(ctx, ip); err == nil && host != "" {
				hostname = host
			}
		}

		ipStat := domain.IPTrafficStat{
			IP:             ip,
			MACAddress:     macAddress,
			Hostname:       hostname,
			UploadRate:     uploadRateMbps,
			DownloadRate:   downloadRateMbps,
			IsLimited:      isLimited,
			BandwidthLimit: bandwidthLimit,
			Status:         "Active",
		}

		// Store for history saving
		allIPStats = append(allIPStats, ipStat)

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
	globalStat := s.sendGlobalStats(ctx)

	// =========================================================
	// 5. Save Traffic History (async)
	// =========================================================
	if s.trafficHistoryRepo != nil && globalStat != nil {
		go func() {
			historyCtx := context.Background() // Use background context to prevent cancellation
			if err := s.SaveTrafficSnapshot(historyCtx, globalStat, allIPStats); err != nil {
				log.Printf("Failed to save traffic history: %v", err)
			}
		}()
	}
}

// sendGlobalStats calcule et envoie les statistiques globales des interfaces
func (s *QoSManager) sendGlobalStats(ctx context.Context) *domain.GlobalTrafficStat {
	// Vérifier si le contexte est annulé
	select {
	case <-ctx.Done():
		return nil
	default:
	}

	// Utiliser s.driver directement pour les méthodes globales
	globalDriver, ok := s.driver.(interface {
		GetInstantaneousNetDevStats(iface string) (domain.NetDevStats, error)
		CalculateRateMbps(iface string, currentStats domain.NetDevStats) (txRateMbps float64, rxRateMbps float64, err error)
	})

	if !ok {
		log.Println("Warning: Driver does not support global stats methods")
		return nil
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
	// totalActive = nombre total d'IPs avec trafic détecté (détectées via iptables)
	// totalLimited = nombre d'IPs avec limite personnalisée appliquée par l'utilisateur
	s.ipMutex.RLock()
	totalActive := len(s.detectedIPs) // Nombre total d'IPs détectées avec trafic
	s.ipMutex.RUnlock()

	// Count IPs with user-applied limits from database
	totalLimited := 0
	if s.deviceRepo != nil {
		ctx := context.Background()
		devices, err := s.deviceRepo.ListAll(ctx)
		if err == nil {
			for _, device := range devices {
				// Count devices with non-null bandwidth_limit
				if device.BandwidthLimit != nil && *device.BandwidthLimit != "" {
					totalLimited++
				}
			}
		}
	}

	s.globalSetupMutex.Lock()
	currentGlobalLimit := s.globalLimit
	s.globalSetupMutex.Unlock()

	globalStat := domain.GlobalTrafficStat{
		LanInterface:    s.ilan,
		WanInterface:    s.inet,
		LanUploadRate:   lanRxRate, // LAN Rx = Upload from users
		LanDownloadRate: lanTxRate, // LAN Tx = Download to users
		WanUploadRate:   wanTxRate, // WAN Tx = Upload to internet
		WanDownloadRate: wanRxRate, // WAN Rx = Download from internet
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
		return &globalStat
	case s.statsStream <- update:
		// Sent successfully
	default:
		log.Println("Warning: Stats channel full, dropping global data.")
	}

	return &globalStat
}

// BlockDevice bloque un appareil en utilisant iptables pour empêcher l'accès à Internet
func (s *QoSManager) BlockDevice(ctx context.Context, ip string) error {
	log.Printf("QoSManager: Blocking device %s", ip)

	// Apply iptables block
	if err := s.driver.BlockDevice(ctx, ip, s.inet); err != nil {
		return err
	}

	// Persist blocked status to database
	if err := s.deviceRepo.UpdateBlockedStatus(ctx, ip, true); err != nil {
		log.Printf("Warning: Failed to persist blocked status for %s: %v", ip, err)
		// Don't fail the operation if DB update fails, iptables rule is already applied
	}

	return nil
}

// UnblockDevice débloque un appareil en supprimant les règles iptables
func (s *QoSManager) UnblockDevice(ctx context.Context, ip string) error {
	log.Printf("QoSManager: Unblocking device %s", ip)

	// Remove iptables block
	if err := s.driver.UnblockDevice(ctx, ip, s.inet); err != nil {
		return err
	}

	// Persist unblocked status to database
	if err := s.deviceRepo.UpdateBlockedStatus(ctx, ip, false); err != nil {
		log.Printf("Warning: Failed to persist unblocked status for %s: %v", ip, err)
		// Don't fail the operation if DB update fails, iptables rule is already removed
	}

	return nil
}

// IsDeviceBlocked vérifie si un appareil est actuellement bloqué
func (s *QoSManager) IsDeviceBlocked(ctx context.Context, ip string) (bool, error) {
	return s.driver.IsDeviceBlocked(ctx, ip, s.inet)
}

// RestoreBlockedDevices restores blocked status from database on startup
func (s *QoSManager) RestoreBlockedDevices(ctx context.Context) error {
	log.Println("QoSManager: Restoring blocked devices from database...")

	blockedDevices, err := s.deviceRepo.ListBlocked(ctx)
	if err != nil {
		return fmt.Errorf("failed to list blocked devices: %w", err)
	}

	if len(blockedDevices) == 0 {
		log.Println("QoSManager: No blocked devices to restore")
		return nil
	}

	restored := 0
	failed := 0
	for _, device := range blockedDevices {
		if err := s.driver.BlockDevice(ctx, device.IP, s.inet); err != nil {
			log.Printf("Warning: Failed to restore block for device %s (%s): %v", device.IP, device.MACAddress, err)
			failed++
		} else {
			log.Printf("Restored block for device %s (%s)", device.IP, device.MACAddress)
			restored++
		}
	}

	log.Printf("QoSManager: Restored %d blocked devices (%d failed)", restored, failed)
	return nil
}

// GetLanInterface returns the LAN interface name
func (s *QoSManager) GetLanInterface() string {
	return s.ilan
}

// GetWanInterface returns the WAN interface name
func (s *QoSManager) GetWanInterface() string {
	return s.inet
}

// GetGlobalRateLimit returns the current global bandwidth limit
func (s *QoSManager) GetGlobalRateLimit() string {
	s.globalSetupMutex.Lock()
	defer s.globalSetupMutex.Unlock()
	return s.globalLimit
}

// incrementSequence atomically increments the state sequence counter
func (s *QoSManager) incrementSequence() {
	s.sequenceMutex.Lock()
	s.stateSequence++
	s.sequenceMutex.Unlock()
}

// getSequence returns the current sequence number
func (s *QoSManager) getSequence() uint64 {
	s.sequenceMutex.Lock()
	defer s.sequenceMutex.Unlock()
	return s.stateSequence
}

// IPSnapshot represents the current state of tracked IPs for sync
type IPSnapshot struct {
	Sequence    uint64                 `json:"sequence"`
	Timestamp   time.Time              `json:"timestamp"`
	GlobalLimit string                 `json:"global_limit"`
	IPs         []domain.IPTrafficStat `json:"ips"`
}

// CurrentIPSnapshot returns the current state of all tracked IPs with sequence number
func (s *QoSManager) CurrentIPSnapshot() IPSnapshot {
	s.ipMutex.RLock()
	defer s.ipMutex.RUnlock()

	ips := make([]domain.IPTrafficStat, 0, len(s.activeIPLimits))
	for ip, info := range s.activeIPLimits {
		// Get current stats for this IP
		ipStat := domain.IPTrafficStat{
			IP:             ip,
			UploadRate:     0.0, // Will be updated by monitoring loop
			DownloadRate:   0.0,
			IsLimited:      info.Rule.Bandwidth != DefaultIPRate,
			BandwidthLimit: info.Rule.Bandwidth,
			Status:         "Active",
		}
		ips = append(ips, ipStat)
	}

	return IPSnapshot{
		Sequence:    s.getSequence(),
		Timestamp:   time.Now(),
		GlobalLimit: s.globalLimit,
		IPs:         ips,
	}
}

// SaveTrafficSnapshot saves current traffic stats to history database
func (s *QoSManager) SaveTrafficSnapshot(ctx context.Context, globalStat *domain.GlobalTrafficStat, ipStats []domain.IPTrafficStat) error {
	if s.trafficHistoryRepo == nil {
		return nil // History tracking disabled
	}

	timestamp := time.Now()

	// Save global traffic history
	if globalStat != nil {
		historyEntry := &domain.TrafficHistoryEntry{
			Timestamp:       timestamp,
			LanUploadRate:   globalStat.LanUploadRate,
			LanDownloadRate: globalStat.LanDownloadRate,
			WanUploadRate:   globalStat.WanUploadRate,
			WanDownloadRate: globalStat.WanDownloadRate,
			IPStats:         make([]domain.IPTrafficHistoryStat, 0, len(ipStats)),
		}

		// Add simplified IP stats for JSONB storage
		for _, ipStat := range ipStats {
			historyEntry.IPStats = append(historyEntry.IPStats, domain.IPTrafficHistoryStat{
				IP:           ipStat.IP,
				UploadRate:   ipStat.UploadRate,
				DownloadRate: ipStat.DownloadRate,
				MACAddress:   ipStat.MACAddress,
			})
		}

		if err := s.trafficHistoryRepo.SaveGlobalTraffic(ctx, historyEntry); err != nil {
			log.Printf("Failed to save global traffic history: %v", err)
			// Don't return error, just log it
		}
	}

	// Save per-IP traffic history
	if len(ipStats) > 0 {
		ipEntries := make([]*domain.IPTrafficHistoryEntry, 0, len(ipStats))
		for _, ipStat := range ipStats {
			ipEntries = append(ipEntries, &domain.IPTrafficHistoryEntry{
				Timestamp:      timestamp,
				IP:             ipStat.IP,
				MACAddress:     ipStat.MACAddress,
				Hostname:       ipStat.Hostname,
				UploadRate:     ipStat.UploadRate,
				DownloadRate:   ipStat.DownloadRate,
				BandwidthLimit: ipStat.BandwidthLimit,
			})
		}

		if err := s.trafficHistoryRepo.SaveIPTraffic(ctx, ipEntries); err != nil {
			log.Printf("Failed to save IP traffic history: %v", err)
		}
	}

	return nil
}

/* func() */
