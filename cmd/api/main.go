package main

import (
	"context"
	"database/sql"
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
	"bandwidth_controller_backend/internal/core/port"
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

	// 3. Clean up interfaces before setup
	log.Printf("Cleaning up interface %s and %s on startup...", wanInterface, lanInterface)
	if err := networkDriver.ResetShaping(cleanupCtx, lanInterface, wanInterface); err != nil {
		log.Printf("Warning: Could not clean up existing QoS rules (may be normal if none exist): %v", err)
	} else {
		log.Println("Interface cleaned successfully.")
	}

	// 4. Initialize Database Connection FIRST
	dbConfig := config.GetDatabaseConfig()
	db, err := config.ConnectDatabase(dbConfig)

	// Initialize device repository
	var deviceRepo port.DeviceRepository
	var trafficHistoryRepo port.TrafficHistoryRepository
	if err != nil {
		log.Printf("Warning: Could not connect to PostgreSQL: %v", err)
		log.Println("Device persistence disabled (no database)")
		deviceRepo = nil // QoSManager will handle nil gracefully
		trafficHistoryRepo = nil
	} else {
		log.Println("PostgreSQL connection successful")
		deviceRepo = adapter.NewPostgresDeviceRepository(db)
		trafficHistoryRepo = adapter.NewPostgresTrafficHistoryRepository(db)

		// Run device table migration
		if err := runDeviceMigration(db); err != nil {
			log.Printf("Warning: Device migration failed: %v", err)
		}

		// Run traffic history migration
		if err := runTrafficHistoryMigration(db); err != nil {
			log.Printf("Warning: Traffic history migration failed: %v", err)
		}
	}

	// 5. Create QoSManager with device repository from the start
	qosService := service.NewQoSManager(networkDriver, deviceRepo, trafficHistoryRepo, lanInterface, wanInterface)

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

	// 6. Initialize Scheduler
	bandwidthScheduler := service.NewBandwidthScheduler(qosService)
	bandwidthScheduler.Start()
	log.Println("Bandwidth scheduler initialized")
	// 7. Initialize Auth Handler
	var authHandler *handler.AuthHandler
	if err != nil {
		log.Println("Falling back to in-memory auth repository")
		authRepo := adapter.NewInMemoryAuthRepository()
		authService := service.NewAuthService(authRepo)
		authHandler = handler.NewAuthHandler(authService)
	} else {
		authRepo := adapter.NewPostgresAuthRepository(db)
		authService := service.NewAuthService(authRepo)
		authHandler = handler.NewAuthHandler(authService)
	}

	// 8. Initialize Network Handler (AFTER QoSManager has device repo)
	qosHandler := handler.NewNetworkHandler(qosService, networkDriver, trafficHistoryRepo, lanInterface, wanInterface)

	// 9. Setup Routes
	setupRoutes(qosHandler, authHandler, bandwidthScheduler)

	port := "8080"
	log.Printf("Server listening on port %s...", port)
}

// runDeviceMigration creates the devices table if it doesn't exist
func runDeviceMigration(db *sql.DB) error {
	migrationSQL := `
	CREATE TABLE IF NOT EXISTS devices (
		id SERIAL PRIMARY KEY,
		ip VARCHAR(45) NOT NULL UNIQUE,
		mac_address VARCHAR(17) NOT NULL,
		bandwidth_limit VARCHAR(20) DEFAULT NULL,
		device_name VARCHAR(255) DEFAULT NULL,
		last_seen TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
		updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
	);
	
	CREATE INDEX IF NOT EXISTS idx_devices_ip ON devices(ip);
	CREATE INDEX IF NOT EXISTS idx_devices_mac ON devices(mac_address);
	CREATE INDEX IF NOT EXISTS idx_devices_last_seen ON devices(last_seen);
	`

	_, err := db.Exec(migrationSQL)
	if err != nil {
		return fmt.Errorf("failed to create devices table: %w", err)
	}

	log.Println("Devices table migration completed")
	return nil
}

// runTrafficHistoryMigration creates the traffic history tables if they don't exist
func runTrafficHistoryMigration(db *sql.DB) error {
	migrationSQL := `
	-- Create traffic history table for storing historical bandwidth data
	CREATE TABLE IF NOT EXISTS traffic_history (
		id BIGSERIAL PRIMARY KEY,
		timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		
		-- Global traffic stats
		lan_upload_rate DECIMAL(10, 4),      -- Mbps
		lan_download_rate DECIMAL(10, 4),    -- Mbps
		wan_upload_rate DECIMAL(10, 4),      -- Mbps
		wan_download_rate DECIMAL(10, 4),    -- Mbps
		
		-- Per-IP traffic stats (aggregated)
		ip_stats JSONB,                      -- Array of {ip, upload_rate, download_rate, mac_address}
		
		created_at TIMESTAMPTZ DEFAULT NOW()
	);

	-- Create index on timestamp for efficient time-range queries
	CREATE INDEX IF NOT EXISTS idx_traffic_history_timestamp ON traffic_history(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_traffic_history_created_at ON traffic_history(created_at);

	-- Create a separate table for detailed IP traffic history
	CREATE TABLE IF NOT EXISTS ip_traffic_history (
		id BIGSERIAL PRIMARY KEY,
		timestamp TIMESTAMPTZ NOT NULL DEFAULT NOW(),
		ip VARCHAR(45) NOT NULL,              -- IPv4 or IPv6
		mac_address VARCHAR(17),              -- MAC address
		hostname VARCHAR(255),                -- Device hostname
		upload_rate DECIMAL(10, 4),           -- Mbps
		download_rate DECIMAL(10, 4),         -- Mbps
		bandwidth_limit VARCHAR(20),          -- e.g., "25mbit"
		
		created_at TIMESTAMPTZ DEFAULT NOW()
	);

	-- Create indexes for efficient queries
	CREATE INDEX IF NOT EXISTS idx_ip_traffic_timestamp ON ip_traffic_history(timestamp DESC);
	CREATE INDEX IF NOT EXISTS idx_ip_traffic_ip ON ip_traffic_history(ip);
	CREATE INDEX IF NOT EXISTS idx_ip_traffic_ip_timestamp ON ip_traffic_history(ip, timestamp DESC);
	`

	_, err := db.Exec(migrationSQL)
	if err != nil {
		return fmt.Errorf("failed to create traffic history tables: %w", err)
	}

	log.Println("Traffic history tables migration completed")
	return nil
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
	protected.GET("/qos/ips", qosHandler.GetIPsSnapshotHandler)
	protected.GET("/status/lanips", qosHandler.GetConnectedLANIPsHandler)
	log.Println("Monitoring routes initialized")

	// Routes de Device Control (Block/Unblock)
	protected.POST("/qos/device/block", qosHandler.BlockDeviceHandler)
	protected.POST("/qos/device/unblock", qosHandler.UnblockDeviceHandler)
	protected.GET("/qos/device/status", qosHandler.GetDeviceStatusHandler)
	log.Println("Device control routes initialized")

	// Routes de Traffic History
	protected.GET("/traffic/history/global", qosHandler.GetGlobalTrafficHistoryHandler)
	protected.GET("/traffic/history/ip", qosHandler.GetIPTrafficHistoryHandler)
	protected.GET("/traffic/history/top-consumers", qosHandler.GetTopConsumersHandler)
	log.Println("Traffic history routes initialized")

	// Routes de Scheduling
	handler.RegisterScheduleRoutes(r, bandwidthScheduler)
	log.Println("Bandwidth scheduling routes initialized")

	// Start HTTP Server
	if err := http.ListenAndServe(":8080", r); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}

	return r
}
