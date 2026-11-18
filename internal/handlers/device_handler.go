package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/jhmeshu/zkteco-restful-api/internal/config"
	"github.com/jhmeshu/zkteco-restful-api/internal/devicemanager"
	"github.com/jhmeshu/zkteco-restful-api/internal/zkservice"
)

type DeviceHandler struct {
	cfg     *config.Config
	manager *devicemanager.DeviceManager
}

func NewDeviceHandler(cfg *config.Config, manager *devicemanager.DeviceManager) *DeviceHandler {
	return &DeviceHandler{cfg: cfg, manager: manager}
}

func (h *DeviceHandler) getService(c *gin.Context, explicitID string) (*zkservice.ZkService, *devicemanager.DeviceItem, error) {
	deviceID := explicitID
	if deviceID == "" {
		deviceID = c.Param("device_id")
	}
	if deviceID == "" {
		deviceID = c.Query("device_id")
	}
	return h.manager.Get(deviceID)
}

// ListDevices handles GET /devices
func (h *DeviceHandler) ListDevices(c *gin.Context) {
	statuses := h.manager.GetStatuses(c.Request.Context())
	c.JSON(http.StatusOK, gin.H{
		"message": "Devices retrieved successfully",
		"data":    statuses,
	})
}

type AddDeviceRequest struct {
	ID       string `json:"id"`
	Name     string `json:"name" binding:"required"`
	IP       string `json:"ip" binding:"required"`
	Port     int    `json:"port"`
	Password int    `json:"password"`
}

// AddDevice handles POST /devices
func (h *DeviceHandler) AddDevice(c *gin.Context) {
	var req AddDeviceRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.Port <= 0 {
		req.Port = 4370
	}

	item := devicemanager.DeviceItem{
		ID:       req.ID,
		Name:     req.Name,
		IP:       req.IP,
		Port:     req.Port,
		Password: req.Password,
	}

	if err := h.manager.Add(item); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Device added successfully",
		"data":    item,
	})
}

// DeleteDevice handles DELETE /devices/:device_id
func (h *DeviceHandler) DeleteDevice(c *gin.Context) {
	id := c.Param("device_id")
	if id == "" {
		id = c.Param("id")
	}
	if id == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "device ID is required"})
		return
	}

	if err := h.manager.Remove(id); err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{"message": "Device removed successfully"})
}

// DeviceCapture handles GET /device/capture
func (h *DeviceHandler) DeviceCapture(c *gin.Context) {
	srv, item, err := h.getService(c, "")
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"message": err.Error()})
		return
	}

	info, err := srv.Ping(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": fmt.Sprintf("Error connecting to device '%s' (%s): %s", item.Name, item.IP, err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message": "Device connected successfully",
		"data": gin.H{
			"id":            item.ID,
			"name":          item.Name,
			"serial_number": info.SerialNumber,
			"firmware":      info.Firmware,
			"platform":      info.Platform,
			"device_name":   info.DeviceName,
			"ip":            item.IP,
			"port":          item.Port,
		},
	})
}

// RebootDevice handles POST /devices/:device_id/reboot and POST /device/reboot
func (h *DeviceHandler) RebootDevice(c *gin.Context) {
	var body struct {
		DeviceID string `json:"device_id"`
	}
	_ = c.ShouldBindJSON(&body)

	srv, item, err := h.getService(c, body.DeviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	if err := srv.Restart(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": fmt.Sprintf("Failed to reboot device '%s': %s", item.Name, err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   fmt.Sprintf("Reboot command sent to '%s'. Device is restarting.", item.Name),
		"device_id": item.ID,
	})
}

// ClearAttendance handles POST /devices/:device_id/clear-attendance and POST /device/clear-attendance
func (h *DeviceHandler) ClearAttendance(c *gin.Context) {
	var body struct {
		DeviceID string `json:"device_id"`
	}
	_ = c.ShouldBindJSON(&body)

	srv, item, err := h.getService(c, body.DeviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	if err := srv.ClearAttendance(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": fmt.Sprintf("Failed to clear attendance on '%s': %s", item.Name, err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   fmt.Sprintf("Attendance punch logs cleared successfully on '%s'.", item.Name),
		"device_id": item.ID,
	})
}

// RefreshCache handles POST /devices/:device_id/refresh-cache and POST /device/refresh-cache
func (h *DeviceHandler) RefreshCache(c *gin.Context) {
	var body struct {
		DeviceID string `json:"device_id"`
	}
	_ = c.ShouldBindJSON(&body)

	srv, item, err := h.getService(c, body.DeviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	if err := srv.RefreshData(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": fmt.Sprintf("Failed to refresh cache on '%s': %s", item.Name, err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   fmt.Sprintf("Internal device cache and biometrics refreshed successfully on '%s'.", item.Name),
		"device_id": item.ID,
	})
}

// WipeDevice handles POST /devices/:device_id/wipe and POST /device/wipe
func (h *DeviceHandler) WipeDevice(c *gin.Context) {
	var body struct {
		DeviceID string `json:"device_id"`
	}
	_ = c.ShouldBindJSON(&body)

	srv, item, err := h.getService(c, body.DeviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	if err := srv.ClearAllData(c.Request.Context()); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": fmt.Sprintf("Failed to wipe data on '%s': %s", item.Name, err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   fmt.Sprintf("All data (users, biometrics, punches) wiped successfully on '%s'.", item.Name),
		"device_id": item.ID,
	})
}

type DeviceCommandRequest struct {
	Command  string `json:"command" binding:"required"`
	DeviceID string `json:"device_id"`
}

// ExecuteCommand handles unified POST /devices/:device_id/command and POST /device/command
func (h *DeviceHandler) ExecuteCommand(c *gin.Context) {
	var req DeviceCommandRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "command is required (reboot, clean_punches, clean_cache, wipe)"})
		return
	}

	srv, item, err := h.getService(c, req.DeviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	switch strings.ToLower(req.Command) {
	case "reboot", "restart":
		if err := srv.Restart(ctx); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message":   fmt.Sprintf("Reboot command sent to '%s'. Terminal is restarting.", item.Name),
			"command":   "reboot",
			"device_id": item.ID,
		})

	case "clean_punches", "clear_attendance", "clean_attendance", "data_clean":
		if err := srv.ClearAttendance(ctx); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message":   fmt.Sprintf("Attendance punch logs cleared successfully on '%s'.", item.Name),
			"command":   "clean_punches",
			"device_id": item.ID,
		})

	case "clean_cache", "refresh_cache", "refresh_data", "cache_clean":
		if err := srv.RefreshData(ctx); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message":   fmt.Sprintf("Device caches and biometrics refreshed successfully on '%s'.", item.Name),
			"command":   "clean_cache",
			"device_id": item.ID,
		})

	case "wipe", "data_wipe", "wipe_device", "clear_all_data":
		if err := srv.ClearAllData(ctx); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"message": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{
			"message":   fmt.Sprintf("Complete data wipe finished on '%s' (all users, biometrics, punches deleted).", item.Name),
			"command":   "data_wipe",
			"device_id": item.ID,
		})

	default:
		c.JSON(http.StatusBadRequest, gin.H{
			"error": fmt.Sprintf("Unknown command '%s'. Supported: reboot, clean_punches, clean_cache, data_wipe", req.Command),
		})
	}
}
