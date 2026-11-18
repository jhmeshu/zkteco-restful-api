package zkservice

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"
	"time"

	"github.com/senthilnasa/zkgo"
	"github.com/senthilnasa/zkgo/models"
	"github.com/senthilnasa/zkgo/protocol"
	"github.com/senthilnasa/zkgo/transport"
)

type ZkService struct {
	IP       string
	Port     int
	Password int
	mu       sync.Mutex
	device   zk.Device
}

func NewZkService(ip string, port int, password int) *ZkService {
	return &ZkService{
		IP:       ip,
		Port:     port,
		Password: password,
	}
}

// EnsureConnected checks if the device is connected; if not, attempts to connect.
func (s *ZkService) EnsureConnected(ctx context.Context) (zk.Device, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.device != nil {
		return s.device, nil
	}

	addr := fmt.Sprintf("%s:%d", s.IP, s.Port)
	log.Printf("[ZK] Connecting to ZKTeco device at %s...", addr)

	dialCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	dev, err := zk.Dial(dialCtx, zk.Config{
		Addr:     addr,
		Password: uint32(s.Password),
		Timeout:  4 * time.Second,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to ZKTeco device on %s: %w", addr, err)
	}

	log.Printf("[ZK] Successfully connected to ZKTeco device at %s", addr)
	s.device = dev
	return s.device, nil
}

// ResetConnection drops the cached connection so the next call reconnects.
func (s *ZkService) ResetConnection() {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.device != nil {
		_ = s.device.Close()
		s.device = nil
	}
}

// Close gracefully closes the device connection.
func (s *ZkService) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.device != nil {
		err := s.device.Close()
		s.device = nil
		return err
	}
	return nil
}

// Ping checks if the device is responsive and returns its info.
func (s *ZkService) Ping(ctx context.Context) (models.DeviceInfo, error) {
	dev, err := s.EnsureConnected(ctx)
	if err != nil {
		return models.DeviceInfo{}, err
	}

	info, err := dev.Info(ctx)
	if err != nil {
		s.ResetConnection()
		return models.DeviceInfo{}, fmt.Errorf("ping error: %w", err)
	}
	return info, nil
}

// GetAllUsers retrieves all enrolled users from the device.
func (s *ZkService) GetAllUsers(ctx context.Context) ([]models.User, error) {
	dev, err := s.EnsureConnected(ctx)
	if err != nil {
		return nil, err
	}

	users, err := zk.Collect(dev.Users(ctx))
	if err != nil {
		s.ResetConnection()
		return nil, fmt.Errorf("failed to get users: %w", err)
	}
	return users, nil
}

// CreateOrUpdateUser creates or updates a user on the device.
func (s *ZkService) CreateOrUpdateUser(ctx context.Context, u models.User) error {
	dev, err := s.EnsureConnected(ctx)
	if err != nil {
		return err
	}

	cmd, err := dev.SetUser(ctx, u)
	if err != nil {
		s.ResetConnection()
		return fmt.Errorf("failed to set user: %w", err)
	}

	if cmd != nil {
		if err := cmd.Err(); err != nil {
			return fmt.Errorf("set user command failed: %w", err)
		}
	}
	return nil
}

// DeleteUser deletes a user by their user ID.
func (s *ZkService) DeleteUser(ctx context.Context, userID string) error {
	dev, err := s.EnsureConnected(ctx)
	if err != nil {
		return err
	}

	cmd, err := dev.DeleteUser(ctx, userID)
	if err != nil {
		s.ResetConnection()
		return fmt.Errorf("failed to delete user: %w", err)
	}

	if cmd != nil {
		if err := cmd.Err(); err != nil {
			return fmt.Errorf("delete user command failed: %w", err)
		}
	}
	return nil
}

// resolveUserSerial resolves a UserID string into internal UserSerial uint16.
func (s *ZkService) resolveUserSerial(ctx context.Context, dev zk.Device, userID string) (uint16, error) {
	for u, err := range dev.Users(ctx) {
		if err != nil {
			break
		}
		if u.UserID == userID {
			return u.SerialNo, nil
		}
	}

	if num, err := strconv.ParseUint(userID, 10, 16); err == nil {
		return uint16(num), nil
	}

	return 0, fmt.Errorf("user %s not found on device", userID)
}

// EnrollFingerprint puts the physical terminal into enrollment mode for a given finger (0-9).
func (s *ZkService) EnrollFingerprint(ctx context.Context, userID string, tempID uint8) error {
	dev, err := s.EnsureConnected(ctx)
	if err != nil {
		return err
	}

	bd, ok := dev.(zk.BiometricDevice)
	if !ok {
		return errors.New("connected device does not support biometric enrollment")
	}

	_, err = bd.Enroll(ctx, zk.EnrollOptions{
		UserID:  userID,
		Index:   tempID,
		Kind:    models.BiometricFingerprint,
		Timeout: 60 * time.Second,
	})
	if err != nil {
		return fmt.Errorf("fingerprint enrollment failed or timed out: %w", err)
	}
	return nil
}

// EnrollFace triggers facial enrollment on a face recognition terminal like SpeedFace (index 111).
func (s *ZkService) EnrollFace(ctx context.Context, userID string) error {
	addr := fmt.Sprintf("%s:%d", s.IP, s.Port)

	dev, err := s.EnsureConnected(ctx)
	if err != nil {
		return err
	}

	// Verify the user exists on the device
	if _, err := s.resolveUserSerial(ctx, dev, userID); err != nil {
		return err
	}

	// Connect dedicated TCP session for the face enrollment command
	rawConn, err := net.DialTimeout("tcp", addr, 4*time.Second)
	if err != nil {
		return fmt.Errorf("failed to dial device for face enrollment: %w", err)
	}
	defer rawConn.Close()

	tcpConn := transport.NewTCPConn(rawConn, 4*time.Second)
	sess := protocol.NewSession(tcpConn)

	connCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	if err := sess.Connect(connCtx, uint32(s.Password)); err != nil {
		return fmt.Errorf("device authentication failed: %w", err)
	}

	// 1. Cancel any active verification or finger capture loop on the terminal
	_, _ = sess.Do(ctx, protocol.CmdCancelCapture, nil)

	// 2. Build 26-byte StartEnrollEx payload for face recognition:
	// Format: [24-byte null-padded UserID string, 1-byte temp_id (111), 1-byte flag (0)]
	// Note: A 4-byte buffer [uint16 serial, uint8 index, 0] causes firmware to interpret
	// index as finger_id (111 % 10 = finger 1) and triggers the optical fingerprint scanner.
	// The 26-byte buffer is recognized by SpeedFace/TFT firmware as StartEnrollEx with index 111 (Face).
	req := make([]byte, 26)
	copy(req[0:24], []byte(userID))
	req[24] = 111 // Universal Face enrollment index
	req[25] = 0   // Flag: 0 = standard enrollment

	resp, err := sess.Do(ctx, protocol.CmdStartEnroll, req)
	if err != nil {
		return fmt.Errorf("face enrollment trigger failed: %w", err)
	}
	if !resp.Command.IsACK() {
		return fmt.Errorf("device rejected face enrollment (code %v)", resp.Command)
	}

	// 3. Put device into 1:N matching / live screen capture state
	_, _ = sess.Do(ctx, protocol.CmdStartVerify, nil)

	// 4. Signal firmware to refresh screen state
	_, _ = sess.Do(ctx, protocol.CmdRefreshData, nil)

	// 5. Cleanly exit session so terminal UI is unlocked for interactive enrollment
	_, _ = sess.Do(ctx, protocol.CmdExit, nil)

	log.Printf("[ZK] Successfully activated face scan camera on %s for user %s (index 111)", addr, userID)
	return nil
}

// GetFingerprintTemplate fetches a user's fingerprint template from the device.
func (s *ZkService) GetFingerprintTemplate(ctx context.Context, userID string, tempID uint8) (*models.Template, error) {
	dev, err := s.EnsureConnected(ctx)
	if err != nil {
		return nil, err
	}

	bd, ok := dev.(zk.BiometricDevice)
	if !ok {
		return nil, errors.New("connected device does not support biometric templates")
	}

	userSerial, err := s.resolveUserSerial(ctx, dev, userID)
	if err != nil {
		return nil, err
	}

	tmpl, err := bd.Template(ctx, userSerial, tempID)
	if err != nil {
		return nil, fmt.Errorf("failed to get template: %w", err)
	}
	return &tmpl, nil
}

// DeleteFingerprintTemplate deletes a user's fingerprint template from the device.
func (s *ZkService) DeleteFingerprintTemplate(ctx context.Context, userID string, tempID uint8) error {
	dev, err := s.EnsureConnected(ctx)
	if err != nil {
		return err
	}

	bd, ok := dev.(zk.BiometricDevice)
	if !ok {
		return errors.New("connected device does not support biometric templates")
	}

	userSerial, err := s.resolveUserSerial(ctx, dev, userID)
	if err != nil {
		return err
	}

	cmd, err := bd.DeleteTemplate(ctx, userSerial, tempID)
	if err != nil {
		return fmt.Errorf("failed to delete template: %w", err)
	}

	if cmd != nil {
		if err := cmd.Err(); err != nil {
			return fmt.Errorf("delete template command failed: %w", err)
		}
	}
	return nil
}

// SubscribeEvents initiates a realtime event subscription with a long-lived connection.
func (s *ZkService) SubscribeEvents(ctx context.Context) (*zk.Subscription, error) {
	addr := fmt.Sprintf("%s:%d", s.IP, s.Port)

	// Use a 24-hour timeout for the event subscription so that inactivity timeouts
	// in zkgo don't cause constant connection churn every 4 seconds.
	subDev, err := zk.Dial(ctx, zk.Config{
		Addr:     addr,
		Password: uint32(s.Password),
		Timeout:  24 * time.Hour,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to dial device for event subscription: %w", err)
	}

	sub, err := subDev.Subscribe(ctx, zk.AllEvents)
	if err != nil {
		_ = subDev.Close()
		return nil, fmt.Errorf("failed to subscribe to events: %w", err)
	}
	return sub, nil
}

// Restart reboots the physical terminal.
func (s *ZkService) Restart(ctx context.Context) error {
	dev, err := s.EnsureConnected(ctx)
	if err != nil {
		return err
	}

	cmd, err := dev.Restart(ctx)
	s.ResetConnection() // Drop cached connection since terminal is restarting
	if err != nil {
		return fmt.Errorf("failed to send restart command: %w", err)
	}
	if cmd != nil {
		if err := cmd.Err(); err != nil {
			return fmt.Errorf("restart failed: %w", err)
		}
	}
	return nil
}

// ClearAttendance erases all attendance punch logs from the terminal memory.
func (s *ZkService) ClearAttendance(ctx context.Context) error {
	dev, err := s.EnsureConnected(ctx)
	if err != nil {
		return err
	}

	cmd, err := dev.ClearAttendance(ctx)
	if err != nil {
		return fmt.Errorf("failed to clear attendance logs: %w", err)
	}
	if cmd != nil {
		if err := cmd.Err(); err != nil {
			return fmt.Errorf("clear attendance command failed: %w", err)
		}
	}
	return nil
}

// RefreshData requests the device to reload its internal caches and biometrics.
func (s *ZkService) RefreshData(ctx context.Context) error {
	dev, err := s.EnsureConnected(ctx)
	if err != nil {
		return err
	}

	ctrl, ok := dev.(zk.ControlDevice)
	if !ok {
		return errors.New("device does not implement ControlDevice interface")
	}

	cmd, err := ctrl.RefreshData(ctx)
	if err != nil {
		return fmt.Errorf("failed to refresh device cache: %w", err)
	}
	if cmd != nil {
		if err := cmd.Err(); err != nil {
			return fmt.Errorf("refresh data command failed: %w", err)
		}
	}
	return nil
}

// ClearAllData wipes all enrolled users, biometric templates, and attendance records from the device.
func (s *ZkService) ClearAllData(ctx context.Context) error {
	dev, err := s.EnsureConnected(ctx)
	if err != nil {
		return err
	}

	ctrl, ok := dev.(zk.ControlDevice)
	if !ok {
		return errors.New("device does not implement ControlDevice interface")
	}

	cmd, err := ctrl.ClearAllData(ctx)
	s.ResetConnection() // Reset connection after full wipe
	if err != nil {
		return fmt.Errorf("failed to wipe device data: %w", err)
	}
	if cmd != nil {
		if err := cmd.Err(); err != nil {
			return fmt.Errorf("clear all data command failed: %w", err)
		}
	}
	return nil
}
