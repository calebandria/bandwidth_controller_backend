package service

import (
	"bandwidth_controller_backend/internal/core/domain"
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// BandwidthScheduler gère les règles de scheduling de bande passante avec cron
type BandwidthScheduler struct {
	qos        *QoSManager
	cron       *cron.Cron
	mu         sync.RWMutex
	rules      []domain.ScheduleRule
	entryIDs   map[string]cron.EntryID // map rule ID -> cron entry ID
	activeJobs map[string]*time.Timer  // map rule ID -> timer pour durée
}

// NewBandwidthScheduler crée un nouveau scheduler avec go-cron
func NewBandwidthScheduler(qos *QoSManager) *BandwidthScheduler {
	return &BandwidthScheduler{
		qos:        qos,
		cron:       cron.New(),
		rules:      []domain.ScheduleRule{},
		entryIDs:   make(map[string]cron.EntryID),
		activeJobs: make(map[string]*time.Timer),
	}
}

// Start démarre le scheduler cron
func (s *BandwidthScheduler) Start() {
	s.cron.Start()
	log.Println("[Scheduler] Bandwidth scheduler started")
}

// Stop arrête le scheduler et tous les timers actifs
func (s *BandwidthScheduler) Stop() {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Arrêter tous les timers actifs
	for _, timer := range s.activeJobs {
		timer.Stop()
	}
	s.activeJobs = make(map[string]*time.Timer)

	// Arrêter le cron
	ctx := s.cron.Stop()
	<-ctx.Done()
	log.Println("[Scheduler] Bandwidth scheduler stopped")
}

// GetRules retourne toutes les règles configurées
func (s *BandwidthScheduler) GetRules() []domain.ScheduleRule {
	s.mu.RLock()
	defer s.mu.RUnlock()
	rules := make([]domain.ScheduleRule, len(s.rules))
	copy(rules, s.rules)
	return rules
}

// SetRules remplace toutes les règles et reconfigure le cron
func (s *BandwidthScheduler) SetRules(rules []domain.ScheduleRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Supprimer toutes les anciennes entrées cron
	for _, entryID := range s.entryIDs {
		s.cron.Remove(entryID)
	}
	s.entryIDs = make(map[string]cron.EntryID)

	// Arrêter tous les timers actifs
	for _, timer := range s.activeJobs {
		timer.Stop()
	}
	s.activeJobs = make(map[string]*time.Timer)

	// Sauvegarder les nouvelles règles
	s.rules = make([]domain.ScheduleRule, len(rules))
	copy(s.rules, rules)

	// Ajouter les nouvelles règles activées au cron
	for _, rule := range s.rules {
		if rule.Enabled {
			if err := s.addRuleToCron(rule); err != nil {
				log.Printf("[Scheduler] Error adding rule %s: %v", rule.ID, err)
				return err
			}
		}
	}

	log.Printf("[Scheduler] Rules updated: %d active rules", len(s.entryIDs))
	return nil
}

// AddRule ajoute une nouvelle règle
func (s *BandwidthScheduler) AddRule(rule domain.ScheduleRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Vérifier si la règle existe déjà
	for _, r := range s.rules {
		if r.ID == rule.ID {
			return fmt.Errorf("rule with ID %s already exists", rule.ID)
		}
	}

	s.rules = append(s.rules, rule)

	if rule.Enabled {
		if err := s.addRuleToCron(rule); err != nil {
			return err
		}
	}

	log.Printf("[Scheduler] Rule added: %s", rule.ID)
	return nil
}

// RemoveRule supprime une règle par son ID
func (s *BandwidthScheduler) RemoveRule(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, rule := range s.rules {
		if rule.ID == id {
			// Supprimer du cron si elle y est
			if entryID, exists := s.entryIDs[id]; exists {
				s.cron.Remove(entryID)
				delete(s.entryIDs, id)
			}

			// Arrêter le timer si actif
			if timer, exists := s.activeJobs[id]; exists {
				timer.Stop()
				delete(s.activeJobs, id)
			}

			// Supprimer de la liste
			s.rules = append(s.rules[:i], s.rules[i+1:]...)
			log.Printf("[Scheduler] Rule removed: %s", id)
			return true
		}
	}

	return false
}

// addRuleToCron ajoute une règle au scheduler cron (doit être appelé avec le lock)
func (s *BandwidthScheduler) addRuleToCron(rule domain.ScheduleRule) error {
	entryID, err := s.cron.AddFunc(rule.CronExpr, func() {
		s.executeRule(rule)
	})

	if err != nil {
		return fmt.Errorf("invalid cron expression '%s': %v", rule.CronExpr, err)
	}

	s.entryIDs[rule.ID] = entryID
	log.Printf("[Scheduler] Rule '%s' scheduled with cron: %s", rule.Name, rule.CronExpr)
	return nil
}

// executeRule exécute une règle (applique le rate et configure la durée)
func (s *BandwidthScheduler) executeRule(rule domain.ScheduleRule) {
	log.Printf("[Scheduler] Executing rule: %s (rate: %d Mbps, duration: %d min)", rule.Name, rule.RateMbps, rule.Duration)

	// Appliquer la limite de bande passante
	qosRule := domain.QoSRule{
		Bandwidth: fmt.Sprintf("%dMbit", rule.RateMbps),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.qos.UpdateGlobalLimit(ctx, qosRule); err != nil {
		log.Printf("[Scheduler] Error applying rule %s: %v", rule.ID, err)
		return
	}

	log.Printf("[Scheduler] ✓ Applied %d Mbps for rule '%s'", rule.RateMbps, rule.Name)

	// Si une durée est spécifiée, programmer le retour à une limite par défaut
	if rule.Duration > 0 {
		s.mu.Lock()
		// Arrêter le timer précédent si existant
		if oldTimer, exists := s.activeJobs[rule.ID]; exists {
			oldTimer.Stop()
		}

		// Créer un nouveau timer pour restaurer la limite après la durée
		timer := time.AfterFunc(time.Duration(rule.Duration)*time.Minute, func() {
			log.Printf("[Scheduler] Duration expired for rule '%s', you may want to restore default rate", rule.Name)
			// Note: Vous pouvez implémenter une restauration automatique ici
			s.mu.Lock()
			delete(s.activeJobs, rule.ID)
			s.mu.Unlock()
		})

		s.activeJobs[rule.ID] = timer
		s.mu.Unlock()
	}
}

// GetNextScheduledTime retourne la prochaine exécution d'une règle
func (s *BandwidthScheduler) GetNextScheduledTime(ruleID string) *time.Time {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if entryID, exists := s.entryIDs[ruleID]; exists {
		entry := s.cron.Entry(entryID)
		if !entry.Next.IsZero() {
			next := entry.Next
			return &next
		}
	}

	return nil
}
