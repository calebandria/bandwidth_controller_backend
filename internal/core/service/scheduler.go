package service

import (
	"bandwidth_controller_backend/internal/core/domain"
	"bandwidth_controller_backend/internal/core/port"
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	"github.com/robfig/cron/v3"
)

// BandwidthScheduler gère les règles de scheduling de bande passante avec cron
type BandwidthScheduler struct {
	qos            *QoSManager
	repo           port.ScheduleRepository
	cron           *cron.Cron
	mu             sync.RWMutex
	rules          []domain.ScheduleRule
	entryIDs       map[string]cron.EntryID // map rule ID -> cron entry ID
	activeJobs     map[string]*time.Timer  // map rule ID -> timer pour durée
	previousLimits map[string]string       // map rule ID -> previous bandwidth limit
}

// NewBandwidthScheduler crée un nouveau scheduler avec go-cron
func NewBandwidthScheduler(qos *QoSManager, repo port.ScheduleRepository) *BandwidthScheduler {
	return &BandwidthScheduler{
		qos:            qos,
		repo:           repo,
		cron:           cron.New(),
		rules:          []domain.ScheduleRule{},
		entryIDs:       make(map[string]cron.EntryID),
		activeJobs:     make(map[string]*time.Timer),
		previousLimits: make(map[string]string),
	}
}

// Start démarre le scheduler cron et charge les règles depuis la base de données
func (s *BandwidthScheduler) Start() {
	// Load rules from database
	ctx := context.Background()
	rules, err := s.repo.GetAll(ctx)
	if err != nil {
		log.Printf("[Scheduler] Error loading rules from database: %v", err)
	} else {
		// Load rules into memory and start enabled ones
		if err := s.SetRules(rules); err != nil {
			log.Printf("[Scheduler] Error setting loaded rules: %v", err)
		} else {
			log.Printf("[Scheduler] Loaded %d rules from database", len(rules))

			// Check if any rules should be active right now
			s.checkActiveRules()
		}
	}

	s.cron.Start()
	log.Println("[Scheduler] Bandwidth scheduler started")
}

// checkActiveRules checks if any scheduled rules should be currently active
func (s *BandwidthScheduler) checkActiveRules() {
	s.mu.RLock()
	rulesToCheck := make([]domain.ScheduleRule, len(s.rules))
	copy(rulesToCheck, s.rules)
	s.mu.RUnlock()

	now := time.Now()
	for _, rule := range rulesToCheck {
		if !rule.Enabled {
			continue
		}

		if s.isRuleActiveNow(rule, now) {
			log.Printf("[Scheduler] Rule '%s' should be active now, applying immediately", rule.Name)
			go s.executeRule(rule)
		}
	}
}

// isRuleActiveNow checks if a rule should be active at the given time
func (s *BandwidthScheduler) isRuleActiveNow(rule domain.ScheduleRule, now time.Time) bool {
	// Parse cron expression to get scheduled time
	parts := rule.CronExpr
	// Simple parsing for our format: "minute hour * * days"
	var minute, hour int
	var days string
	fmt.Sscanf(parts, "%d %d * * %s", &minute, &hour, &days)

	// Check if today matches the days pattern
	currentDay := int(now.Weekday())
	dayMatches := false

	if days == "*" {
		dayMatches = true
	} else if days == "1-5" && currentDay >= 1 && currentDay <= 5 {
		dayMatches = true
	} else if days == "0,6" && (currentDay == 0 || currentDay == 6) {
		dayMatches = true
	}

	if !dayMatches {
		return false
	}

	// Calculate scheduled start and end times
	scheduledStart := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, now.Location())
	scheduledEnd := scheduledStart.Add(time.Duration(rule.Duration) * time.Minute)

	// Check if current time is within the scheduled window
	return now.After(scheduledStart) && now.Before(scheduledEnd)
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

// AddRule ajoute une nouvelle règle et la persiste dans la base de données
func (s *BandwidthScheduler) AddRule(rule domain.ScheduleRule) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Vérifier si la règle existe déjà
	for _, r := range s.rules {
		if r.ID == rule.ID {
			return fmt.Errorf("rule with ID %s already exists", rule.ID)
		}
	}

	// Persist to database
	ctx := context.Background()
	if err := s.repo.Create(ctx, &rule); err != nil {
		return fmt.Errorf("failed to save rule to database: %w", err)
	}

	s.rules = append(s.rules, rule)

	if rule.Enabled {
		if err := s.addRuleToCron(rule); err != nil {
			return err
		}
	}

	log.Printf("[Scheduler] Rule added and persisted: %s", rule.ID)
	return nil
}

// RemoveRule supprime une règle par son ID et de la base de données
func (s *BandwidthScheduler) RemoveRule(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	for i, rule := range s.rules {
		if rule.ID == id {
			// Delete from database
			ctx := context.Background()
			if err := s.repo.Delete(ctx, id); err != nil {
				log.Printf("[Scheduler] Error deleting rule from database: %v", err)
				return false
			}

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
			log.Printf("[Scheduler] Rule removed and deleted from database: %s", id)
			return true
		}
	}

	return false
}

// UpdateRule updates an existing rule and persists changes
func (s *BandwidthScheduler) UpdateRule(rule domain.ScheduleRule) error {
	s.mu.Lock()

	// Find the rule
	found := false
	var shouldApplyNow bool
	var ruleToApply domain.ScheduleRule

	for i, r := range s.rules {
		if r.ID == rule.ID {
			found = true

			// Update in database
			ctx := context.Background()
			if err := s.repo.Update(ctx, &rule); err != nil {
				return fmt.Errorf("failed to update rule in database: %w", err)
			}

			// Remove from cron if it was there
			if entryID, exists := s.entryIDs[rule.ID]; exists {
				s.cron.Remove(entryID)
				delete(s.entryIDs, rule.ID)
			}

			// Stop active timer if exists and restore previous limit if disabling
			if timer, exists := s.activeJobs[rule.ID]; exists {
				timer.Stop()
				delete(s.activeJobs, rule.ID)

				// If disabling the rule and it was active, restore the previous limit
				if !rule.Enabled {
					if previousLimit, hasPrevious := s.previousLimits[rule.ID]; hasPrevious {
						log.Printf("[Scheduler] Disabling active rule '%s', restoring previous limit: %s", rule.Name, previousLimit)
						delete(s.previousLimits, rule.ID)

						// Restore in a goroutine to avoid blocking
						go func() {
							restoreRule := domain.QoSRule{
								LanInterface: s.qos.GetLanInterface(),
								WanInterface: s.qos.GetWanInterface(),
								Bandwidth:    previousLimit,
							}
							ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
							defer cancel()

							if err := s.qos.UpdateGlobalLimit(ctx, restoreRule); err != nil {
								log.Printf("[Scheduler] Error restoring limit after disabling rule: %v", err)
							} else {
								log.Printf("[Scheduler] Restored previous limit: %s", previousLimit)
							}
						}()
					}
				}
			}

			// Update in memory
			s.rules[i] = rule

			// Check if we need to apply the rule immediately
			if rule.Enabled && s.isRuleActiveNow(rule, time.Now()) {
				shouldApplyNow = true
				ruleToApply = rule
			}

			// Add to cron if enabled
			if rule.Enabled {
				if err := s.addRuleToCron(rule); err != nil {
					s.mu.Unlock()
					return err
				}
			}

			log.Printf("[Scheduler] Rule updated and persisted: %s", rule.ID)
			found = true
			break
		}
	}

	s.mu.Unlock()

	if !found {
		return fmt.Errorf("rule not found: %s", rule.ID)
	}

	// Apply immediately if needed (outside the lock)
	if shouldApplyNow {
		log.Printf("[Scheduler] Re-enabled rule '%s' is currently in its time window, applying immediately", ruleToApply.Name)
		s.executeRule(ruleToApply)
	}

	return nil
}

// addRuleToCron ajoute une règle au scheduler cron (doit être appelé avec le lock)
func (s *BandwidthScheduler) addRuleToCron(rule domain.ScheduleRule) error {
	ruleID := rule.ID // Capture just the ID

	entryID, err := s.cron.AddFunc(rule.CronExpr, func() {
		// Get the current rule from memory to ensure we have the latest version
		s.mu.RLock()
		var currentRule *domain.ScheduleRule
		for _, r := range s.rules {
			if r.ID == ruleID {
				currentRule = &r
				break
			}
		}
		s.mu.RUnlock()

		if currentRule == nil {
			log.Printf("[Scheduler] Rule %s not found, skipping execution", ruleID)
			return
		}

		if !currentRule.Enabled {
			log.Printf("[Scheduler] Rule %s is disabled, skipping execution", ruleID)
			return
		}

		s.executeRule(*currentRule)
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
	log.Printf("[Scheduler] Executing rule: %s (priority: %d, rate: %d Mbps, duration: %d min)", rule.Name, rule.Priority, rule.RateMbps, rule.Duration)

	// Check for conflicting higher-priority rules
	s.mu.Lock()
	hasHigherPriority := false
	var higherPriorityRule *domain.ScheduleRule

	for ruleID := range s.activeJobs {
		for _, r := range s.rules {
			if r.ID == ruleID && r.Priority > rule.Priority {
				hasHigherPriority = true
				higherPriorityRule = &r
				break
			}
		}
		if hasHigherPriority {
			break
		}
	}

	if hasHigherPriority && higherPriorityRule != nil {
		s.mu.Unlock()
		log.Printf("[Scheduler] Rule '%s' (priority %d) skipped - higher priority rule '%s' (priority %d) is active",
			rule.Name, rule.Priority, higherPriorityRule.Name, higherPriorityRule.Priority)
		return
	} // Check if we need to override lower priority rules
	lowerPriorityRules := []string{}
	for ruleID := range s.activeJobs {
		for _, r := range s.rules {
			if r.ID == ruleID && r.Priority < rule.Priority {
				lowerPriorityRules = append(lowerPriorityRules, r.Name)
			}
		}
	}

	if len(lowerPriorityRules) > 0 {
		log.Printf("[Scheduler] Rule '%s' (priority %d) overriding lower priority rules: %v",
			rule.Name, rule.Priority, lowerPriorityRules)
	} // Only save current limit if we're not already running this rule
	_, alreadyActive := s.activeJobs[rule.ID]
	if !alreadyActive {
		currentLimit := s.qos.GetGlobalRateLimit()
		s.previousLimits[rule.ID] = currentLimit
		log.Printf("[Scheduler] Saved current limit: %s (for rule: %s)", currentLimit, rule.ID)
	} else {
		log.Printf("[Scheduler] Rule %s is already active, keeping existing previous limit", rule.ID)
	}
	s.mu.Unlock()

	// Appliquer la limite de bande passante avec les interfaces du QoSManager
	qosRule := domain.QoSRule{
		LanInterface: s.qos.GetLanInterface(),
		WanInterface: s.qos.GetWanInterface(),
		Bandwidth:    fmt.Sprintf("%dMbit", rule.RateMbps),
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := s.qos.UpdateGlobalLimit(ctx, qosRule); err != nil {
		log.Printf("[Scheduler] Error applying rule %s: %v", rule.ID, err)
		return
	}

	log.Printf("[Scheduler] Applied %d Mbps for rule '%s'", rule.RateMbps, rule.Name)

	// Si une durée est spécifiée, programmer le retour à la limite précédente
	if rule.Duration > 0 {
		s.mu.Lock()
		// Arrêter le timer précédent si existant
		if oldTimer, exists := s.activeJobs[rule.ID]; exists {
			oldTimer.Stop()
		}

		// Créer un nouveau timer pour restaurer la limite après la durée
		timer := time.AfterFunc(time.Duration(rule.Duration)*time.Minute, func() {
			log.Printf("[Scheduler] Duration expired for rule '%s' (priority %d)", rule.Name, rule.Priority)

			// Get the previous limit
			s.mu.Lock()
			previousLimit, exists := s.previousLimits[rule.ID]
			delete(s.activeJobs, rule.ID)
			delete(s.previousLimits, rule.ID)

			// Check if there's a lower-priority rule that should now apply
			var nextRule *domain.ScheduleRule
			highestPriority := 0
			now := time.Now()

			for _, r := range s.rules {
				if !r.Enabled || r.ID == rule.ID {
					continue
				}

				// Check if this rule should be active now
				if s.isRuleActiveNow(r, now) && r.Priority > highestPriority {
					nextRule = &r
					highestPriority = r.Priority
				}
			}
			s.mu.Unlock()

			// If there's another rule that should apply, apply it instead of restoring
			if nextRule != nil {
				log.Printf("[Scheduler] Applying next highest priority rule '%s' (priority %d) after '%s' expired",
					nextRule.Name, nextRule.Priority, rule.Name)
				go s.executeRule(*nextRule)
				return
			}

			if !exists {
				log.Printf("[Scheduler] No previous limit found for rule %s, skipping restore", rule.ID)
				return
			}

			// Restore the previous limit
			restoreRule := domain.QoSRule{
				LanInterface: s.qos.GetLanInterface(),
				WanInterface: s.qos.GetWanInterface(),
				Bandwidth:    previousLimit,
			}

			restoreCtx, restoreCancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer restoreCancel()

			if err := s.qos.UpdateGlobalLimit(restoreCtx, restoreRule); err != nil {
				log.Printf("[Scheduler] Error restoring previous limit for rule %s: %v", rule.ID, err)
				return
			}

			log.Printf("[Scheduler] Restored previous limit: %s for rule '%s'", previousLimit, rule.Name)
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
