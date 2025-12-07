package main

import (
	"context"
	"fmt"
	"github.com/gin-gonic/gin"
	"log"
	"net/http"
	"os"
	"time"

	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	_ "bandwidth_controller_backend/docs" 
	"bandwidth_controller_backend/internal/adapter/handler"
	"bandwidth_controller_backend/internal/adapter/system"
	"bandwidth_controller_backend/internal/core/service"
)
// @title QoS Traffic Control API
// @version 1.0
// @description This API manages Quality of Service (QoS) rules using Linux Traffic Control (tc).
// @host localhost:8080
// @BasePath /
func main() {
	if len(os.Args) < 3 {
		fmt.Println("Please provide the Lan and Wan interfaces in that order.")
		return
	}

	lanInterface := os.Args[1]
	wanInterface := os.Args[2]

	fmt.Println("Program Name:", os.Args[0])
		fmt.Println("Lan interface:", lanInterface)
	fmt.Println("Wan interface:", wanInterface)


	log.Println("Starting QoS Bandwidth Controller...")

	networkDriver := system.NewLinuxDriver()

	qosService := service.NewQoSManager(networkDriver, lanInterface, wanInterface)

	qosHandler := handler.NewNetworkHandler(qosService,networkDriver, lanInterface)

	log.Printf("Cleaning up interface %s and %s on startup...", wanInterface, lanInterface)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := networkDriver.ResetShaping(ctx, lanInterface, wanInterface); err != nil {
		log.Printf("Warning: Could not clean up existing QoS rules (may be normal if none exist): %v", err)
	} else {
		log.Println("Interface cleaned successfully.")
	}


	r := gin.Default()
	// --- Swagger Route ---
	// Serves the Swagger UI at http://localhost:8080/swagger/index.html
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))
    log.Println("Swagger documentation available at /swagger/index.html")
	r.POST("/qos/simple/limit", qosHandler.UpdateSimpleLimit)
	r.POST("/qos/setup", qosHandler.SetupGlobalHandler)
	r.POST("/qos/htb/global/limit", qosHandler.UpdateHTBGlobalLimit)
	r.POST("/qos/reset", qosHandler.ResetShapingHandler)
	r.GET("/status/lanips", qosHandler.GetConnectedLANIPsHandler)
	r.GET("/qos/stats", qosHandler.GetTrafficStatsHandler)
	r.GET("/qos/stream", qosHandler.StreamTrafficStatsHandler)


	port := "8080"
	log.Printf("Server listening on port %s...", port)
	if err := http.ListenAndServe(":"+port, r); err != nil {
		log.Fatalf("Server failed to start: %v", err)
	}
}
