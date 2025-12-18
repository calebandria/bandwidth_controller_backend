package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/joho/godotenv"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "bandwidth_controller_backend/docs"
	"bandwidth_controller_backend/internal/adapter/handler"
	adapter "bandwidth_controller_backend/internal/adapter/repository"
	"bandwidth_controller_backend/internal/adapter/system"
	"bandwidth_controller_backend/internal/config"
	"bandwidth_controller_backend/internal/core/service"
)

// @title QoS Traffic Control API
// @version 1.0
// @description This API manages Quality of Service (QoS) rules using Linux Traffic Control (tc).
// @host localhost:8080
// @BasePath /
func main() {
	// Load .env file
	if err := godotenv.Load(); err != nil {
		log.Println("Warning: .env file not found, using environment variables or defaults")
	}

	if len(os.Args) < 3 {
		fmt.Println("Please provide the Lan and Wan interfaces in that order.")
		return
	}

	globalRateLimit := "100mbit"

	lanInterface := os.Args[1]
	wanInterface := os.Args[2]

	fmt.Println("Program Name:", os.Args[0])
	fmt.Println("Lan interface:", lanInterface)
	fmt.Println("Wan interface:", wanInterface)

	log.Println("Starting QoS Bandwidth Controller...")

	networkDriver := system.NewLinuxDriver()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Contexte court pour le cleanup
	cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cleanupCancel()

	qosService := service.NewQoSManager(networkDriver, lanInterface, wanInterface)

	if err := qosService.SetupGlobalQoS(cleanupCtx, lanInterface, wanInterface, globalRateLimit); err != nil {
		log.Fatalf("Fatal Error: Could not start default global HTB QoS: %v. Exiting.", err)
	} else {
		log.Println("Automatic HTB startup success")

		// ⚠️ ADD THIS DELAY: Give the Linux kernel a moment to commit the complex TC rules.
		log.Println("Waiting 100ms for system commit...")
		time.Sleep(100 * time.Millisecond)
	}

	// UTILISATION DE 'go' POUR EXÉCUTER LA BOUCLE DE MONITORING EN PARALLÈLE
	go qosService.StartIPMonitoring(ctx, 2*time.Second) // Lance le monitoring toutes les 2 secondes

	// Initialiser le Scheduler de bande passante
	bandwidthScheduler := service.NewBandwidthScheduler(qosService)
	bandwidthScheduler.Start()
	log.Println("Bandwidth scheduler initialized")

	// 3. Clean up interfaces before setup
	log.Printf("Cleaning up interface %s and %s on startup...", wanInterface, lanInterface)
	if err := networkDriver.ResetShaping(cleanupCtx, lanInterface, wanInterface); err != nil {
		log.Printf("Warning: Could not clean up existing QoS rules (may be normal if none exist): %v", err)
	} else {
		log.Println("Interface cleaned successfully.")
	}

	// 4. Initialize Network Handler
	qosHandler := handler.NewNetworkHandler(qosService, networkDriver, lanInterface, wanInterface)

	// 5. Initialize Database Connection
	dbConfig := config.GetDatabaseConfig()
	db, err := config.ConnectDatabase(dbConfig)

	var authHandler *handler.AuthHandler
	if err != nil {
		log.Printf("Warning: Could not connect to PostgreSQL: %v", err)
		log.Println("Falling back to in-memory auth repository")
		authRepo := adapter.NewInMemoryAuthRepository()
		authService := service.NewAuthService(authRepo)
		authHandler = handler.NewAuthHandler(authService)
	} else {
		log.Println("PostgreSQL connection successful")
		authRepo := adapter.NewPostgresAuthRepository(db)
		authService := service.NewAuthService(authRepo)
		authHandler = handler.NewAuthHandler(authService)
	}

	// 6. Setup Routes
	setupRoutes(qosHandler, authHandler, bandwidthScheduler)

	port := "8080"
	log.Printf("Server listening on port %s...", port)
}

// setupRoutes configures all HTTP routes
func setupRoutes(qosHandler *handler.NetworkHandler, authHandler *handler.AuthHandler, bandwidthScheduler *service.BandwidthScheduler) *gin.Engine {
	r := gin.Default()

	// CORS Middleware for frontend
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}
		c.Next()
	})

	// --- Swagger Route ---
	// Serves the Swagger UI at http://localhost:8080/swagger/index.html
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
	log.Println("Swagger documentation available at /swagger/index.html")

	// --- Auth Routes (Public) ---
	r.POST("/auth/login", authHandler.Login)
	r.POST("/auth/logout", authHandler.Logout)
	r.GET("/auth/me", authHandler.AuthMiddleware(), authHandler.GetCurrentUser)
	log.Println("Authentication routes initialized")

	// --- Protected Routes Group ---
	protected := r.Group("/")
	// protected.Use(authHandler.AuthMiddleware()) // Uncomment to enable auth protection

	// Routes QoS Globales
	protected.POST("/qos/setup", qosHandler.SetupGlobalHandler)
	protected.POST("/qos/htb/global/limit", qosHandler.UpdateHTBGlobalLimit)
	protected.POST("/qos/simple/limit", qosHandler.UpdateSimpleLimit)
	protected.POST("/qos/reset", qosHandler.ResetShapingHandler)

	// Routes QoS par IP (Nouveaux Endpoints)
	protected.POST("/qos/ip/limit", qosHandler.AddIPRateLimitHandler)
	protected.POST("/qos/ip/remove", qosHandler.RemoveIPRateLimitHandler)

	// Routes de Monitoring (WebSocket et HTTP)
	protected.GET("/qos/stats", qosHandler.GetTrafficStatsHandler)
	protected.GET("/qos/stream", qosHandler.StreamTrafficStatsHandler)
	protected.GET("/status/lanips", qosHandler.GetConnectedLANIPsHandler)
	log.Println("Monitoring routes initialized")

	// Routes de Device Control (Block/Unblock)
	protected.POST("/qos/device/block", qosHandler.BlockDeviceHandler)
	protected.POST("/qos/device/unblock", qosHandler.UnblockDeviceHandler)
	protected.GET("/qos/device/status", qosHandler.GetDeviceStatusHandler)
	log.Println("Device control routes initialized")

	// Routes de Scheduling
	handler.RegisterScheduleRoutes(r, bandwidthScheduler)
	log.Println("Bandwidth scheduling routes initialized")

	// Start HTTP Server
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}

	return r
}
