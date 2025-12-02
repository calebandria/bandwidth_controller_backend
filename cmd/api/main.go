package main

import (
	"context"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"bandwidth_controller_backend/internal/adapter/handler"
	"bandwidth_controller_backend/internal/adapter/system"
	"bandwidth_controller_backend/internal/core/service"
)

func main() {
	log.Println("Starting QoS Bandwidth Controller...")

	// --- 1. Dependency Injection (Wiring the Layers) ---

	// The LinuxDriver implements the port.NetworkDriver interface.
	networkDriver := system.NewLinuxDriver()

	// The QoSManager uses the driver to perform business logic.
	qosService := service.NewQoSManager(networkDriver)

	// The NetworkHandler uses the service to handle API requests.
	qosHandler := handler.NewNetworkHandler(qosService)

	// --- 2. Startup Tasks ---
	// NOTE: We do NOT call SetupGlobalQoS here anymore.
	// The HTB structure is now initialized ONLY via the POST /qos/setup API endpoint.

	// Ensure the network interface is clean on startup.
	// This prevents conflicts if the app crashed previously while TBF or HTB was active.
	const targetInterface = "wlo1"
	log.Printf("Cleaning up interface %s on startup...", targetInterface)
	// We use a short timeout context for startup cleanup
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second) 
	defer cancel()

	if err := networkDriver.ResetShaping(ctx, targetInterface); err != nil {
		log.Printf("Warning: Could not clean up existing QoS rules (may be normal if none exist): %v", err)
	} else {
		log.Println("Interface cleaned successfully.")
	}

	// --- 3. HTTP Server Setup (Gin Router) ---

	r := gin.Default()

	// Global Middleware (optional: add CORS, authentication, etc., here)

	// --- API Routes ---

	// Simple TBF Feature: POST /qos/simple/limit
	// Applies the simple TBF limit (must not be run while HTB is active)
	r.POST("/qos/simple/limit", qosHandler.UpdateSimpleLimit)

	// HTB Feature Setup: POST /qos/setup
	// Initializes the HTB structure and switches the service to HTB mode.
	r.POST("/qos/setup", qosHandler.SetupGlobalHandler)

	// HTB Global Limit: POST /qos/htb/global/limit
	// Modifies the rate of the HTB 1:1 root class (requires HTB to be active).
	r.POST("/qos/htb/global/limit", qosHandler.UpdateHTBGlobalLimit)

	// Monitoring: GET /ws/stats
	// Streams real-time statistics.
	r.GET("/ws/stats", qosHandler.StreamStats)

	// --- 4. Start Server ---

	port := "8080"
	log.Printf("Server listening on port %s...", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}