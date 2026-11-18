package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/kardianos/service"
	"github.com/jhmeshu/zkteco-restful-api/internal/autostart"
	"github.com/jhmeshu/zkteco-restful-api/internal/config"
	"github.com/jhmeshu/zkteco-restful-api/internal/devicemanager"
	"github.com/jhmeshu/zkteco-restful-api/internal/handlers"
	"github.com/jhmeshu/zkteco-restful-api/internal/worker"
	"github.com/jhmeshu/zkteco-restful-api/web"
)

type program struct {
	cfg        *config.Config
	srv        *http.Server
	deviceMgr  *devicemanager.DeviceManager
	liveWorker *worker.MultiLiveCaptureWorker
	cancel     context.CancelFunc
}

func (p *program) Start(s service.Service) error {
	ctx, cancel := context.WithCancel(context.Background())
	p.cancel = cancel
	go p.run(ctx)
	return nil
}

func (p *program) Stop(s service.Service) error {
	if p.cancel != nil {
		p.cancel()
	}
	if p.srv != nil {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = p.srv.Shutdown(shutdownCtx)
	}
	if p.deviceMgr != nil {
		p.deviceMgr.Close()
	}
	return nil
}

func (p *program) run(ctx context.Context) {
	p.cfg = config.LoadConfig()

	if !p.cfg.Debug {
		gin.SetMode(gin.ReleaseMode)
	}

	log.Printf("==================================================")
	log.Printf("Starting ZKTeco REST API & Multi-Device Manager")
	log.Printf("Server Port:   %s", p.cfg.ServerPort)
	log.Printf("Live Capture:  %v", p.cfg.EnableLiveCapture)
	if p.cfg.EnableLiveCapture {
		log.Printf("Backend URL:   %s", p.cfg.BackendURL)
	}
	log.Printf("Web Dashboard: http://localhost:%s/", p.cfg.ServerPort)
	log.Printf("==================================================")

	// Initialize Multi-Device Manager
	p.deviceMgr = devicemanager.NewDeviceManager(p.cfg, "devices.json")

	// Start live attendance capture supervisor if enabled
	if p.cfg.EnableLiveCapture {
		p.liveWorker = worker.NewLiveCaptureWorker(p.cfg, p.deviceMgr)
		go p.liveWorker.Start(ctx)
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
	userHandler := handlers.NewUserHandler(p.deviceMgr)
	deviceHandler := handlers.NewDeviceHandler(p.cfg, p.deviceMgr)

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
			"web_dashboard": fmt.Sprintf("http://localhost:%s/ui", p.cfg.ServerPort),
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
	p.srv = &http.Server{
		Addr:         ":" + p.cfg.ServerPort,
		Handler:      r,
		ReadTimeout:  15 * time.Second,
		WriteTimeout: 15 * time.Second,
	}

	// Run HTTP server
	go func() {
		log.Printf("HTTP REST API listening on port %s", p.cfg.ServerPort)
		if err := p.srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("HTTP server failed: %v", err)
		}
	}()

	// Wait for context termination
	<-ctx.Done()
	log.Println("Shutting down gracefully...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := p.srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("Server forced to shutdown: %v", err)
	}

	log.Println("ZKTeco service exited cleanly")
}

func main() {
	// Always set current working directory to the directory of the executable
	// so that .env and devices.json are found regardless of how Windows launches the process
	if exePath, err := os.Executable(); err == nil {
		_ = os.Chdir(filepath.Dir(exePath))
	}

	silentFlag := flag.Bool("silent", false, "Run in background with hidden console window")
	flag.BoolVar(silentFlag, "s", false, "Alias for --silent")
	flag.Parse()

	// If launched with --silent or from Windows Startup, hide the console window immediately
	if *silentFlag {
		autostart.HideConsoleWindow()
	}

	// Service Configuration
	svcConfig := &service.Config{
		Name:        "ZKTecoRESTfulAPI",
		DisplayName: "ZKTeco Multi-Device RESTful API Service",
		Description: "High-performance REST API gateway and live biometric attendance capture for ZKTeco terminals.",
	}

	prg := &program{}
	s, err := service.New(prg, svcConfig)
	if err != nil {
		log.Fatalf("Service initialization error: %v", err)
	}

	// Handle command line actions: install, uninstall, start, stop, restart, status
	args := flag.Args()
	if len(args) > 0 {
		cmd := strings.ToLower(args[0])
		switch cmd {
		case "install":
			if err := service.Control(s, "install"); err != nil {
				log.Fatalf("Failed to install service: %v", err)
			}
			log.Println("ZKTeco service installed successfully as an automatic Windows Service.")
			log.Println("Run 'zkteco-api.exe start' to start the service immediately.")
			return
		case "uninstall", "remove":
			_ = autostart.Unregister()
			if err := service.Control(s, "uninstall"); err != nil {
				log.Fatalf("Failed to uninstall service: %v", err)
			}
			log.Println("ZKTeco service uninstalled successfully.")
			return
		case "start":
			if err := service.Control(s, "start"); err != nil {
				log.Fatalf("Failed to start service: %v", err)
			}
			log.Println("ZKTeco service started successfully.")
			return
		case "stop":
			if err := service.Control(s, "stop"); err != nil {
				log.Fatalf("Failed to stop service: %v", err)
			}
			log.Println("ZKTeco service stopped successfully.")
			return
		case "restart":
			if err := service.Control(s, "restart"); err != nil {
				log.Fatalf("Failed to restart service: %v", err)
			}
			log.Println("ZKTeco service restarted successfully.")
			return
		case "status":
			status, err := s.Status()
			if err != nil {
				log.Printf("Service status query error: %v", err)
			} else {
				log.Printf("Service status: %v", status)
			}
			return
		}
	}

	// If running interactively on Windows, automatically ensure it's registered in Windows Startup
	// so the user can just run it once and it will automatically start on Windows boot!
	if service.Interactive() {
		_ = autostart.EnsureRegistered()

		// Also handle standard OS interrupt signals gracefully in interactive mode
		sigChan := make(chan os.Signal, 1)
		signal.Notify(sigChan, os.Interrupt, syscall.SIGTERM, syscall.SIGINT)
		go func() {
			<-sigChan
			_ = prg.Stop(s)
			os.Exit(0)
		}()
	}

	// Run service or interactive program
	if err := s.Run(); err != nil {
		log.Fatalf("Service run error: %v", err)
	}
}
