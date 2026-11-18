package handlers

import (
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"github.com/jhmeshu/zkteco-restful-api/internal/devicemanager"
	"github.com/jhmeshu/zkteco-restful-api/internal/zkservice"
	"github.com/senthilnasa/zkgo/models"
)

type UserHandler struct {
	manager *devicemanager.DeviceManager
}

func NewUserHandler(manager *devicemanager.DeviceManager) *UserHandler {
	return &UserHandler{manager: manager}
}

func (h *UserHandler) getService(c *gin.Context, explicitID string) (*zkservice.ZkService, *devicemanager.DeviceItem, error) {
	id := explicitID
	if id == "" {
		id = c.Param("device_id")
	}
	if id == "" {
		id = c.Query("device_id")
	}
	return h.manager.Get(id)
}

type CreateUserRequest struct {
	DeviceID  string         `json:"device_id"`
	Broadcast bool           `json:"broadcast"`
	UserID    any            `json:"user_id" binding:"required"`
	UserData  UserDataFields `json:"user_data" binding:"required"`
}

type UpdateUserRequest struct {
	DeviceID  string  `json:"device_id"`
	Broadcast bool    `json:"broadcast"`
	Name      string  `json:"name" binding:"required"`
	Privilege *int    `json:"privilege"`
	GroupID   *int    `json:"group_id"`
	Card      *uint32 `json:"card"`
}

type UserDataFields struct {
	Name      string  `json:"name" binding:"required"`
	Privilege *int    `json:"privilege"`
	Password  string  `json:"password"`
	GroupID   *int    `json:"group_id"`
	Card      *uint32 `json:"card"`
}

type CreateFingerprintRequest struct {
	DeviceID string `json:"device_id"`
	TempID   *int   `json:"temp_id" binding:"required"`
}

type SerializedUser struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	GroupID   uint8  `json:"groupId"`
	Privilege int    `json:"privilege"`
	Card      uint32 `json:"card"`
}

type SerializedTemplate struct {
	ID       any    `json:"id"`
	FID      uint8  `json:"fid"`
	Valid    int    `json:"valid"`
	Template string `json:"template"`
}

// CreateUser handles POST /user and POST /devices/:device_id/user
func (h *UserHandler) CreateUser(c *gin.Context) {
	var req CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	userIDStr := fmt.Sprintf("%v", req.UserID)
	privilege := 0
	if req.UserData.Privilege != nil {
		privilege = *req.UserData.Privilege
	}

	groupID := 0
	if req.UserData.GroupID != nil {
		groupID = *req.UserData.GroupID
	}

	var card uint32 = 0
	if req.UserData.Card != nil {
		card = *req.UserData.Card
	}

	serialNo := uint16(0)
	if n, err := strconv.ParseUint(userIDStr, 10, 16); err == nil {
		serialNo = uint16(n)
	}

	user := models.User{
		SerialNo:   serialNo,
		UserID:     userIDStr,
		Name:       req.UserData.Name,
		Privilege:  models.Privilege(privilege),
		CardNumber: card,
		GroupID:    uint8(groupID),
		Password:   req.UserData.Password,
		Enabled:    true,
	}

	// If broadcast requested, push to ALL registered devices
	if req.Broadcast {
		results := h.manager.BroadcastUser(c.Request.Context(), user)
		summary := make(map[string]string)
		hasError := false
		for devID, err := range results {
			if err != nil {
				summary[devID] = "Error: " + err.Error()
				hasError = true
			} else {
				summary[devID] = "Success"
			}
		}

		status := http.StatusOK
		if hasError {
			status = http.StatusMultiStatus
		}
		c.JSON(status, gin.H{
			"message": "Broadcast completed",
			"results": summary,
		})
		return
	}

	srv, dev, err := h.getService(c, req.DeviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	if err := srv.CreateOrUpdateUser(c.Request.Context(), user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": fmt.Sprintf("Error creating user on device '%s': %s", dev.Name, err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "User added successfully",
		"device_id": dev.ID,
	})
}

// UpdateUser handles PUT /user/:user_id and PUT /devices/:device_id/user/:user_id
func (h *UserHandler) UpdateUser(c *gin.Context) {
	userID := c.Param("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	var req UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	privilege := 0
	if req.Privilege != nil {
		privilege = *req.Privilege
	}

	groupID := 1
	if req.GroupID != nil {
		groupID = *req.GroupID
	}

	var card uint32 = 0
	if req.Card != nil {
		card = *req.Card
	}

	serialNo := uint16(0)
	if n, err := strconv.ParseUint(userID, 10, 16); err == nil {
		serialNo = uint16(n)
	}

	user := models.User{
		SerialNo:   serialNo,
		UserID:     userID,
		Name:       req.Name,
		Privilege:  models.Privilege(privilege),
		CardNumber: card,
		GroupID:    uint8(groupID),
		Enabled:    true,
	}

	if req.Broadcast {
		results := h.manager.BroadcastUser(c.Request.Context(), user)
		summary := make(map[string]string)
		for devID, err := range results {
			if err != nil {
				summary[devID] = "Error: " + err.Error()
			} else {
				summary[devID] = "Success"
			}
		}
		c.JSON(http.StatusOK, gin.H{
			"message": "User updated across all devices",
			"results": summary,
		})
		return
	}

	srv, dev, err := h.getService(c, req.DeviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	if err := srv.CreateOrUpdateUser(c.Request.Context(), user); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": fmt.Sprintf("Error updating user on device '%s': %s", dev.Name, err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "User updated successfully",
		"device_id": dev.ID,
		"user": gin.H{
			"id":      user.UserID,
			"name":    user.Name,
			"groupId": user.GroupID,
		},
	})
}

// GetAllUsers handles GET /users and GET /devices/:device_id/users
func (h *UserHandler) GetAllUsers(c *gin.Context) {
	srv, dev, err := h.getService(c, "")
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	users, err := srv.GetAllUsers(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": fmt.Sprintf("Error retrieving users from device '%s': %s", dev.Name, err.Error()),
		})
		return
	}

	if len(users) == 0 {
		c.JSON(http.StatusOK, gin.H{
			"message":   "No users found",
			"device_id": dev.ID,
			"data":      []SerializedUser{},
		})
		return
	}

	serialized := make([]SerializedUser, 0, len(users))
	for _, u := range users {
		serialized = append(serialized, SerializedUser{
			ID:        u.UserID,
			Name:      u.Name,
			GroupID:   u.GroupID,
			Privilege: int(u.Privilege),
			Card:      u.CardNumber,
		})
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Users retrieved successfully",
		"device_id": dev.ID,
		"data":      serialized,
	})
}

// DeleteUser handles DELETE /user/:user_id and DELETE /devices/:device_id/user/:user_id
func (h *UserHandler) DeleteUser(c *gin.Context) {
	userID := c.Param("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	srv, dev, err := h.getService(c, "")
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	if err := srv.DeleteUser(c.Request.Context(), userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": fmt.Sprintf("Error deleting user from device '%s': %s", dev.Name, err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "User deleted successfully",
		"device_id": dev.ID,
	})
}

// CreateFingerprint handles POST /user/:user_id/fingerprint
func (h *UserHandler) CreateFingerprint(c *gin.Context) {
	userID := c.Param("user_id")
	var req CreateFingerprintRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	if req.TempID == nil || *req.TempID < 0 || *req.TempID > 9 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "temp_id must be between 0 and 9"})
		return
	}

	srv, dev, err := h.getService(c, req.DeviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	if err := srv.EnrollFingerprint(c.Request.Context(), userID, uint8(*req.TempID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": fmt.Sprintf("Error creating fingerprint on device '%s': %s", dev.Name, err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Fingerprint created successfully",
		"device_id": dev.ID,
	})
}

// GetFingerprint handles GET /user/:user_id/fingerprint/:temp_id
func (h *UserHandler) GetFingerprint(c *gin.Context) {
	userID := c.Param("user_id")
	tempIDStr := c.Param("temp_id")

	tempID, err := strconv.Atoi(tempIDStr)
	if err != nil || tempID < 0 || tempID > 9 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid temp_id, must be 0-9"})
		return
	}

	srv, dev, err := h.getService(c, "")
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	tmpl, err := srv.GetFingerprintTemplate(c.Request.Context(), userID, uint8(tempID))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": fmt.Sprintf("Error retrieving fingerprint: %s", err.Error()),
		})
		return
	}

	if tmpl == nil {
		c.JSON(http.StatusOK, gin.H{
			"message":   "No templates found",
			"device_id": dev.ID,
			"data":      "",
		})
		return
	}

	templateStr := base64.StdEncoding.EncodeToString(tmpl.Data)

	c.JSON(http.StatusOK, gin.H{
		"message":   "Fingerprint retrieved successfully",
		"device_id": dev.ID,
		"data": SerializedTemplate{
			ID:       userID,
			FID:      tmpl.Index,
			Valid:    1,
			Template: templateStr,
		},
	})
}

// DeleteFingerprint handles DELETE /user/:user_id/fingerprint/:temp_id
func (h *UserHandler) DeleteFingerprint(c *gin.Context) {
	userID := c.Param("user_id")
	tempIDStr := c.Param("temp_id")

	tempID, err := strconv.Atoi(tempIDStr)
	if err != nil || tempID < 0 || tempID > 9 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid temp_id, must be 0-9"})
		return
	}

	srv, dev, err := h.getService(c, "")
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	if err := srv.DeleteFingerprintTemplate(c.Request.Context(), userID, uint8(tempID)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": fmt.Sprintf("Error deleting fingerprint from device '%s': %s", dev.Name, err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Fingerprint deleted successfully",
		"device_id": dev.ID,
	})
}

// CreateFace handles POST /user/:user_id/face and POST /devices/:device_id/user/:user_id/face
func (h *UserHandler) CreateFace(c *gin.Context) {
	userID := c.Param("user_id")
	if userID == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "user_id is required"})
		return
	}

	var body struct {
		DeviceID string `json:"device_id"`
	}
	_ = c.ShouldBindJSON(&body)

	srv, dev, err := h.getService(c, body.DeviceID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}

	if err := srv.EnrollFace(c.Request.Context(), userID); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"message": fmt.Sprintf("Error triggering face scan on device '%s': %s", dev.Name, err.Error()),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"message":   "Face scan activated on terminal. Please look at the camera.",
		"device_id": dev.ID,
		"user_id":   userID,
	})
}
