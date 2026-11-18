package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jhmeshu/zkteco-restful-api/internal/config"
	"github.com/jhmeshu/zkteco-restful-api/internal/devicemanager"
	"github.com/jhmeshu/zkteco-restful-api/internal/handlers"
	"github.com/jhmeshu/zkteco-restful-api/internal/worker"
	"github.com/jhmeshu/zkteco-restful-api/web"
)

func main() {
	cfg := config.LoadConfig()

	if !cfg.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	log.Printf("==================================================")
	log.Printf("Starting ZKTeco REST API & Multi-Device Manager")
	log.Printf("Server Port:   %s", cfg.ServerPort)
	log.Printf("Live Capture:  %v", cfg.EnableLiveCapture)
	if cfg.EnableLiveCapture {
		log.Printf("Backend URL:   %s", cfg.BackendURL)
	}
	log.Printf("Web Dashboard: http://localhost:%s/", cfg.ServerPort)
	log.Printf("==================================================")

	// Create root context that listens for OS termination signals
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	// Initialize Multi-Device Manager
	deviceMgr := devicemanager.NewDeviceManager(cfg, "devices.json")
	defer func() {
		deviceMgr.Close()
	}()

	// Start live attendance capture supervisor if enabled
	if cfg.EnableLiveCapture {
		captureWorker := worker.NewLiveCaptureWorker(cfg, deviceMgr)
		go captureWorker.Start(ctx)
	} else {
		log.Println("[LiveCapture] Disabled (no BACKEND_URL provided or ENABLE_LIVE_CAPTURE=false)")
	}

	// Set up Gin Router
	r := gin.Default()

	// CORS middleware for web clients
	r.Use(func(c *gin.Context) {
		c.Writer.Header().Set("Access-Control-Allow-Origin", "*")
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Content-Length, Accept-Encoding, X-CSRF-Token, Authorization, accept, origin, Cache-Control, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "POST, OPTIONS, GET, PUT, DELETE")

		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}
		c.Next()
	})

	// Handlers
	userHandler := handlers.NewUserHandler(deviceMgr)
	deviceHandler := handlers.NewDeviceHandler(cfg, deviceMgr)

	// Web GUI & Root API Endpoint
	renderRoot := func(c *gin.Context) {
		accept := c.GetHeader("Accept")
		if strings.Contains(accept, "text/html") || strings.Contains(accept, "*/*") {
			c.Data(http.StatusOK, "text/html; charset=utf-8", web.IndexHTML)
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"service":       "ZKTeco Multi-Device RESTful API",
			"status":        "online",
			"web_dashboard": fmt.Sprintf("http://localhost:%s/ui", cfg.ServerPort),
			"endpoints": gin.H{
				"root":               "GET /",
				"dashboard":          "GET /ui",
				"health":             "GET /health",
				"devices":            "GET /devices, POST /devices, DELETE /devices/:id",
				"device_capture":     "GET /device/capture",
				"list_users":         "GET /users or GET /devices/:device_id/users",
				"create_user":        "POST /user or POST /devices/:device_id/user",
				"delete_user":        "DELETE /user/:user_id",
				"broadcast_user":     "POST /users/broadcast",
				"create_fingerprint": "POST /user/:user_id/fingerprint",
				"get_fingerprint":    "GET /user/:user_id/fingerprint/:temp_id",
				"delete_fingerprint": "DELETE /user/:user_id/fingerprint/:temp_id",
				"create_face":        "POST /user/:user_id/face or POST /devices/:device_id/user/:user_id/face",
				"reboot":             "POST /devices/:device_id/reboot or POST /device/reboot",
				"clear_attendance":   "POST /devices/:device_id/clear-attendance (Data Clean)",
				"refresh_cache":      "POST /devices/:device_id/refresh-cache (Cache Clean)",
				"wipe_device":        "POST /devices/:device_id/wipe (Data Wipe)",
				"device_command":     "POST /devices/:device_id/command (unified)",
			},
		})
	}

	r.GET("/", renderRoot)
	r.GET("/ui", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", web.IndexHTML)
	})
	r.GET("/dashboard", func(c *gin.Context) {
		c.Data(http.StatusOK, "text/html; charset=utf-8", web.IndexHTML)
	})

	// Health check endpoint
	r.GET("/health", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{
			"status": "healthy",
			"time":   time.Now().Format(time.RFC3339),
		})
	})

	// Device Management Routes
	r.GET("/devices", deviceHandler.ListDevices)
	r.POST("/devices", deviceHandler.AddDevice)
	r.DELETE("/devices/:device_id", deviceHandler.DeleteDevice)
	r.GET("/device/capture", deviceHandler.DeviceCapture)

	// Device Control & Maintenance Commands
	r.POST("/devices/:device_id/reboot", deviceHandler.RebootDevice)
	r.POST("/device/reboot", deviceHandler.RebootDevice)
	r.POST("/devices/:device_id/clear-attendance", deviceHandler.ClearAttendance)
	r.POST("/device/clear-attendance", deviceHandler.ClearAttendance)
	r.POST("/devices/:device_id/refresh-cache", deviceHandler.RefreshCache)
	r.POST("/device/refresh-cache", deviceHandler.RefreshCache)
	r.POST("/devices/:device_id/wipe", deviceHandler.WipeDevice)
	r.POST("/device/wipe", deviceHandler.WipeDevice)
	r.POST("/devices/:device_id/command", deviceHandler.ExecuteCommand)
	r.POST("/device/command", deviceHandler.ExecuteCommand)

	// User Routes (Device-scoped or Default)
	r.GET("/users", userHandler.GetAllUsers)
	r.POST("/user", userHandler.CreateUser)
	r.PUT("/user/:user_id", userHandler.UpdateUser)
	r.DELETE("/user/:user_id", userHandler.DeleteUser)
	r.POST("/users/broadcast", userHandler.CreateUser) // with broadcast=true

	// Device-specific User Routes
	r.GET("/devices/:device_id/users", userHandler.GetAllUsers)
	r.POST("/devices/:device_id/user", userHandler.CreateUser)
	r.PUT("/devices/:device_id/user/:user_id", userHandler.UpdateUser)
	r.DELETE("/devices/:device_id/user/:user_id", userHandler.DeleteUser)

	// Fingerprint routes
	r.POST("/user/:user_id/fingerprint", userHandler.CreateFingerprint)
	r.GET("/user/:user_id/fingerprint/:temp_id", userHandler.GetFingerprint)
	r.DELETE("/user/:user_id/fingerprint/:temp_id", userHandler.DeleteFingerprint)
	r.POST("/devices/:device_id/user/:user_id/fingerprint", userHandler.CreateFingerprint)

	// Face routes
	r.POST("/user/:user_id/face", userHandler.CreateFace)
	r.POST("/devices/:device_id/user/:user_id/face", userHandler.CreateFace)

	// HTTP Server
	srv := &http.Server{
		Addr:         ":" + cfg.ServerPort,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	// Run HTTP server in a separate goroutine
	go func() {
		log.Printf("HTTP REST API listening on port %s", cfg.ServerPort)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()
	log.Println("Shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("ZKTeco service exited cleanly")
}
