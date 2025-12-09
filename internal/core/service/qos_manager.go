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
/* 	"github.com/go-co-op/gocron/v2" */
)

const (
	ModeGlobalTBF = "TBF"
	ModeGlobalHTB = "HTB"
	DefaultIPRate = "100mbit" // Débit par défaut appliqué aux nouvelles connexions pour le tracking.
)

// IPTrafficStat représente les statistiques de trafic pour une IP donnée.
type IPTrafficStat struct {
	IP           string  `json:"ip"`
	UploadRate   float64 `json:"upload_mbps"`
	DownloadRate float64 `json:"download_mbps"`
	IsLimited    bool    `json:"is_limited"`
	Status       string  `json:"status"` // e.g., "Active", "New", "Disconnected"
}

// IPTrackingInfo stocke les informations nécessaires au suivi de l'état (y compris l'ID de la classe HTB)
type IPTrackingInfo struct {
	ClassID uint16
	Rule    domain.QoSRule
}

// IPRateMonitor définit les méthodes étendues que le driver doit supporter
// pour que le monitoring des statistiques par IP fonctionne.
// Ces méthodes sont celles que le service *suppose* être implémentées par le driver.

type QoSManager struct {
	driver      port.NetworkDriver
	statsStream chan domain.IPTrafficStat // Changement de type pour les statistiques IP
	threshold   uint64
	ilan        string
	inet        string
	activeMode  string
	
	// Suivi des IPs actuellement limitées (et donc avec une classe HTB).
	activeIPLimits map[string]IPTrackingInfo
	ipMutex        sync.RWMutex
}

func NewQoSManager(driver port.NetworkDriver, ilan string, inet string) *QoSManager {
	mgr := &QoSManager{
		driver:      driver,
		statsStream: make(chan domain.IPTrafficStat, 100),
		threshold:   100000000,
		ilan:        ilan,
		inet:        inet,
		activeMode:  ModeGlobalTBF,
		activeIPLimits: make(map[string]IPTrackingInfo),
	}
	return mgr
}

func (s *QoSManager) SetupGlobalQoS(ctx context.Context, ilan string, inet string, maxRate string) error {
	if err := s.driver.SetupHTBStructure(ctx, ilan, inet, maxRate); err != nil {
		return err
	}
	s.activeMode = ModeGlobalHTB
	return nil
}

func (s *QoSManager) UpdateGlobalLimit(ctx context.Context, rule domain.QoSRule) error {
	if s.activeMode != ModeGlobalHTB {
		return errors.New("HTB mode is not active. Please call /qos/setup first")
	}
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
func (s *QoSManager) GetStatsStream() <-chan domain.IPTrafficStat {
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

	// Utilisation de RLock pour lire l'état afin de déterminer les IPs à retirer.
	ipsToRemove := make([]string, 0)
	s.ipMutex.RLock()
	for ip := range s.activeIPLimits {
		if !currentConnectedSet[ip] {
			ipsToRemove = append(ipsToRemove, ip)
		}
	}
	s.ipMutex.RUnlock()

	// --- Traiter les nouvelles IPs ---
	for _, ip := range connectedIPs {
		// Utilisation de RLock pour lire l'état de 'activeIPLimits' et vérifier si l'IP est déjà suivie.
		s.ipMutex.RLock()
		_, isTracked := s.activeIPLimits[ip]
		s.ipMutex.RUnlock()

		if !isTracked {
			// Nouvelle IP détectée, lui appliquer une limite par défaut pour le monitoring.
			defaultRule := domain.QoSRule{
				Bandwidth:    DefaultIPRate,
				LanInterface: s.ilan,
				WanInterface: s.inet,
			}
			log.Printf("New IP detected: %s. Applying default limit (%s) for monitoring.", ip, DefaultIPRate)
			
			// AddIPRateLimit gère son propre Lock()
			if err := s.AddIPRateLimit(ctx, ip, defaultRule); err != nil {
				 log.Printf("Warning: Failed to apply default tracking rule to new IP %s: %v", ip, err)
			}
		}
		detectedIPs[ip] = true // Marquer comme détecté lors de ce cycle
	}

	// --- Traiter les IPs déconnectées ---
	for _, ip := range ipsToRemove {
		log.Printf("IP %s disconnected. Removing rate limit.", ip)
		
		// RemoveIPRateLimit gère son propre Lock()
		if err := s.RemoveIPRateLimit(ctx, ip); err != nil {
			log.Printf("Warning: Failed to remove rate limit for disconnected IP %s: %v", ip, err)
		}
	}

	// =========================================================
	// 3. Collecte et Calcul des Statistiques
	// =========================================================
	
	// Assertion de type pour accéder aux méthodes spécifiques au monitoring.
	// Nous utilisons l'interface IPRateMonitor définie ci-dessus.
	driverMonitor, ok := s.driver.(port.IPRateMonitor)

	if !ok {
		log.Println("Driver does not support advanced IP tracking methods (missing IPRateMonitor interface).")
		return
	}

	// Maintenant, nous récupérons la map interne du driver: IP -> ClassID
	driverIPs := driverMonitor.GetActiveIPs()

	for ip, classID := range driverIPs {
		// --- 3a. Calcul de l'Upload (WAN) ---
		uploadStats, err := driverMonitor.GetInstantaneousClassStats(s.inet, classID)
		uploadRateMbps := 0.0
		if err == nil {
			uploadRateMbps, _ = driverMonitor.CalculateIPRateMbps(ip, s.inet, classID, uploadStats)
		} else {
			log.Printf("Warning: Failed to read WAN stats for %s (1:%x): %v", ip, classID, err)
		}

		// --- 3b. Calcul du Download (LAN) ---
		downloadStats, err := driverMonitor.GetInstantaneousClassStats(s.ilan, classID)
		downloadRateMbps := 0.0
		if err == nil {
			downloadRateMbps, _ = driverMonitor.CalculateIPRateMbps(ip, s.ilan, classID, downloadStats)
		} else {
			log.Printf("Warning: Failed to read LAN stats for %s (1:%x): %v", ip, classID, err)
		}
		
		// --- 4. Diffusion des résultats ---
		// Vérifier si l'IP est activement limitée par une règle personnalisée (pas juste la règle par défaut du moniteur)
		isLimited := false
		
		// Utilisation de RLock pour lire l'état de 'activeIPLimits' en toute sécurité.
		s.ipMutex.RLock()
		if _, ok := s.activeIPLimits[ip]; ok {
			isLimited = true
		}
		s.ipMutex.RUnlock()

		stat := domain.IPTrafficStat{
			IP: ip,
			UploadRate:   uploadRateMbps,
			DownloadRate: downloadRateMbps,
			IsLimited:    isLimited,
			Status:       "Active", 
		}

		// Envoi des statistiques au canal pour le consommateur WebSocket
		select {
		case s.statsStream <- stat:
			// Sent successfully
		default:
			log.Println("Warning: Stats channel full, dropping data.")
		}
	}
}

/* func() */