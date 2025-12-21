package handler

import (
	"bandwidth_controller_backend/internal/core/domain"
	"bandwidth_controller_backend/internal/core/service"
	"net/http"

	"github.com/gin-gonic/gin"
)

// RegisterScheduleRoutes enregistre les routes de scheduling
func RegisterScheduleRoutes(r *gin.Engine, scheduler *service.BandwidthScheduler) {
	api := r.Group("/qos/schedule")
	{
		api.GET("/global", GetGlobalScheduleHandler(scheduler))
		api.POST("/global", SetGlobalScheduleHandler(scheduler))
		api.POST("/global/rule", AddScheduleRuleHandler(scheduler))
		api.PUT("/global/rule/:id", UpdateScheduleRuleHandler(scheduler))
		api.DELETE("/global/:id", DeleteScheduleRuleHandler(scheduler))
		api.GET("/global/:id/next", GetNextScheduleHandler(scheduler))
	}
}

// GetGlobalScheduleHandler retourne toutes les règles de scheduling
// @Summary Get global bandwidth schedule
// @Description Récupère toutes les règles de scheduling configurées
// @Tags Schedule
// @Produce json
// @Success 200 {object} domain.GlobalSchedule
// @Router /qos/schedule/global [get]
func GetGlobalScheduleHandler(scheduler *service.BandwidthScheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		rules := scheduler.GetRules()
		c.JSON(http.StatusOK, domain.GlobalSchedule{Rules: rules})
	}
}

// SetGlobalScheduleHandler remplace toutes les règles de scheduling
// @Summary Set global bandwidth schedule
// @Description Remplace toutes les règles de scheduling par les nouvelles
// @Tags Schedule
// @Accept json
// @Produce json
// @Param schedule body domain.GlobalSchedule true "Schedule rules"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Router /qos/schedule/global [post]
func SetGlobalScheduleHandler(scheduler *service.BandwidthScheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var schedule domain.GlobalSchedule
		if err := c.ShouldBindJSON(&schedule); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format", "details": err.Error()})
			return
		}

		// Valider les règles
		for i, rule := range schedule.Rules {
			if rule.ID == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Rule ID is required", "rule_index": i})
				return
			}
			if rule.CronExpr == "" {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Cron expression is required", "rule_id": rule.ID})
				return
			}
			if rule.RateMbps <= 0 {
				c.JSON(http.StatusBadRequest, gin.H{"error": "Rate must be positive", "rule_id": rule.ID})
				return
			}
		}

		if err := scheduler.SetRules(schedule.Rules); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to set schedule", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":      "success",
			"message":     "Schedule updated successfully",
			"rules_count": len(schedule.Rules),
		})
	}
}

// AddScheduleRuleHandler ajoute une nouvelle règle de scheduling
// @Summary Add a schedule rule
// @Description Ajoute une nouvelle règle de scheduling
// @Tags Schedule
// @Accept json
// @Produce json
// @Param rule body domain.ScheduleRule true "Schedule rule"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Router /qos/schedule/global/rule [post]
func AddScheduleRuleHandler(scheduler *service.BandwidthScheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		var rule domain.ScheduleRule
		if err := c.ShouldBindJSON(&rule); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format", "details": err.Error()})
			return
		}

		// Valider la règle
		if rule.ID == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Rule ID is required"})
			return
		}
		if rule.CronExpr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cron expression is required"})
			return
		}
		if rule.RateMbps <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Rate must be positive"})
			return
		}

		if err := scheduler.AddRule(rule); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to add rule", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"message": "Rule added successfully",
			"rule_id": rule.ID,
		})
	}
}

// UpdateScheduleRuleHandler updates an existing schedule rule
// @Summary Update a schedule rule
// @Description Modifie une règle de scheduling existante
// @Tags Schedule
// @Accept json
// @Produce json
// @Param id path string true "Rule ID"
// @Param rule body domain.ScheduleRule true "Updated schedule rule"
// @Success 200 {object} map[string]interface{}
// @Failure 400 {object} map[string]string
// @Router /qos/schedule/global/rule/{id} [put]
func UpdateScheduleRuleHandler(scheduler *service.BandwidthScheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Rule ID is required in path"})
			return
		}

		var rule domain.ScheduleRule
		if err := c.ShouldBindJSON(&rule); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request format", "details": err.Error()})
			return
		}

		// Ensure ID matches path parameter
		if rule.ID != id {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Rule ID in body must match path parameter"})
			return
		}

		// Valider la règle
		if rule.CronExpr == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Cron expression is required"})
			return
		}
		if rule.RateMbps <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Rate must be positive"})
			return
		}

		if err := scheduler.UpdateRule(rule); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Failed to update rule", "details": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"message": "Rule updated successfully",
			"rule_id": rule.ID,
		})
	}
}

// DeleteScheduleRuleHandler supprime une règle de scheduling
// @Summary Delete a schedule rule
// @Description Supprime une règle de scheduling par son ID
// @Tags Schedule
// @Produce json
// @Param id path string true "Rule ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Router /qos/schedule/global/{id} [delete]
func DeleteScheduleRuleHandler(scheduler *service.BandwidthScheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Rule ID is required"})
			return
		}

		if !scheduler.RemoveRule(id) {
			c.JSON(http.StatusNotFound, gin.H{"error": "Rule not found", "rule_id": id})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"status":  "success",
			"message": "Rule deleted successfully",
			"rule_id": id,
		})
	}
}

// GetNextScheduleHandler retourne la prochaine exécution d'une règle
// @Summary Get next schedule time
// @Description Retourne la prochaine exécution programmée d'une règle
// @Tags Schedule
// @Produce json
// @Param id path string true "Rule ID"
// @Success 200 {object} map[string]interface{}
// @Failure 404 {object} map[string]string
// @Router /qos/schedule/global/{id}/next [get]
func GetNextScheduleHandler(scheduler *service.BandwidthScheduler) gin.HandlerFunc {
	return func(c *gin.Context) {
		id := c.Param("id")
		if id == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "Rule ID is required"})
			return
		}

		nextTime := scheduler.GetNextScheduledTime(id)
		if nextTime == nil {
			c.JSON(http.StatusNotFound, gin.H{"error": "Rule not found or not scheduled", "rule_id": id})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"rule_id":   id,
			"next_time": nextTime.Format("2006-01-02 15:04:05"),
			"next_unix": nextTime.Unix(),
		})
	}
}
