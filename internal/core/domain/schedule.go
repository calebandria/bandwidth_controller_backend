package domain

// ScheduleRule définit une règle de scheduling pour la bande passante globale
type ScheduleRule struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	RateMbps    int    `json:"rate_mbps"`
	CronExpr    string `json:"cron_expr"` // Expression cron (ex: "0 8 * * 1-5" = 8h00 du lundi au vendredi)
	Duration    int    `json:"duration"`  // Durée en minutes pour laquelle appliquer le rate
	Priority    int    `json:"priority"`  // Priorité (1-10, 10 = highest priority, 1 = lowest)
	Enabled     bool   `json:"enabled"`
}

// GlobalSchedule contient toutes les règles de scheduling
type GlobalSchedule struct {
	Rules []ScheduleRule `json:"rules"`
}

// Exemples d'expressions cron:
// "0 8 * * 1-5"    -> 8h00 du lundi au vendredi
// "0 22 * * 0,6"   -> 22h00 le dimanche et samedi
// "0 */2 * * *"    -> Toutes les 2 heures
// "0 0 */2 * *"    -> Tous les 2 jours à minuit
// "0 18 * * *"     -> Tous les jours à 18h00
