package devicemanager

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/jhmeshu/zkteco-restful-api/internal/config"
	"github.com/jhmeshu/zkteco-restful-api/internal/zkservice"
	"github.com/senthilnasa/zkgo/models"
)

type DeviceItem struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	IP       string `json:"ip"`
	Port     int    `json:"port"`
	Password int    `json:"password"`
}

type DeviceStatus struct {
	ID           string `json:"id"`
	Name         string `json:"name"`
	IP           string `json:"ip"`
	Port         int    `json:"port"`
	Online       bool   `json:"online"`
	Model        string `json:"model"`
	SerialNumber string `json:"serial_number"`
	Firmware     string `json:"firmware"`
	Platform     string `json:"platform"`
	UserCount    int    `json:"user_count"`
	Error        string `json:"error,omitempty"`
}

type DeviceManager struct {
	mu          sync.RWMutex
	devices     []DeviceItem
	services    map[string]*zkservice.ZkService
	storagePath string
	cfg         *config.Config
}

func NewDeviceManager(cfg *config.Config, storagePath string) *DeviceManager {
	if storagePath == "" {
		storagePath = "devices.json"
	}

	mgr := &DeviceManager{
		devices:     make([]DeviceItem, 0),
		services:    make(map[string]*zkservice.ZkService),
		storagePath: storagePath,
		cfg:         cfg,
	}

	mgr.loadOrSeed()
	return mgr
}

func (m *DeviceManager) loadOrSeed() {
	m.mu.Lock()
	defer m.mu.Unlock()

	data, err := os.ReadFile(m.storagePath)
	if err == nil && len(data) > 0 {
		var items []DeviceItem
		if err := json.Unmarshal(data, &items); err == nil && len(items) > 0 {
			m.devices = items
			for _, item := range items {
				m.services[item.ID] = zkservice.NewZkService(item.IP, item.Port, item.Password)
			}
			log.Printf("[DeviceManager] Loaded %d devices from %s", len(m.devices), m.storagePath)
			return
		}
	}

	// Seed with default device from config
	defaultIP := m.cfg.DeviceIP
	if defaultIP == "" {
		defaultIP = "172.16.20.240"
	}

	seeds := []DeviceItem{
		{
			ID:       "dev-1",
			Name:     "Main SpeedFace Terminal",
			IP:       defaultIP,
			Port:     m.cfg.DevicePort,
			Password: m.cfg.DevicePass,
		},
	}

	// If 172.16.20.188 was also used, seed it as secondary
	if defaultIP != "172.16.20.188" {
		seeds = append(seeds, DeviceItem{
			ID:       "dev-2",
			Name:     "Secondary Terminal",
			IP:       "172.16.20.188",
			Port:     4370,
			Password: m.cfg.DevicePass,
		})
	}

	m.devices = seeds
	for _, s := range seeds {
		m.services[s.ID] = zkservice.NewZkService(s.IP, s.Port, s.Password)
	}

	_ = m.saveLocked()
	log.Printf("[DeviceManager] Initialized with %d default device(s)", len(m.devices))
}

func (m *DeviceManager) saveLocked() error {
	data, err := json.MarshalIndent(m.devices, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(m.storagePath, data, 0644)
}

func (m *DeviceManager) GetAll() []DeviceItem {
	m.mu.RLock()
	defer m.mu.RUnlock()

	res := make([]DeviceItem, len(m.devices))
	copy(res, m.devices)
	return res
}

func (m *DeviceManager) Get(id string) (*zkservice.ZkService, *DeviceItem, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()

	if len(m.devices) == 0 {
		return nil, nil, errors.New("no devices registered")
	}

	// If no ID or "default", return the first device
	if id == "" || id == "default" {
		dev := m.devices[0]
		return m.services[dev.ID], &dev, nil
	}

	for _, d := range m.devices {
		if d.ID == id {
			return m.services[d.ID], &d, nil
		}
	}

	return nil, nil, fmt.Errorf("device with ID '%s' not found", id)
}

func (m *DeviceManager) Add(item DeviceItem) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	item.IP = strings.TrimSpace(item.IP)
	item.Name = strings.TrimSpace(item.Name)

	if item.IP == "" {
		return errors.New("device IP is required")
	}
	if item.Name == "" {
		item.Name = "Terminal " + item.IP
	}
	if item.Port <= 0 {
		item.Port = 4370
	}
	if item.ID == "" {
		item.ID = fmt.Sprintf("dev-%d", time.Now().UnixNano()%10000)
	}

	// Check for duplicate ID
	for _, d := range m.devices {
		if d.ID == item.ID {
			return fmt.Errorf("device ID '%s' already exists", item.ID)
		}
	}

	m.devices = append(m.devices, item)
	m.services[item.ID] = zkservice.NewZkService(item.IP, item.Port, item.Password)

	return m.saveLocked()
}

func (m *DeviceManager) Remove(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	foundIdx := -1
	for idx, d := range m.devices {
		if d.ID == id {
			foundIdx = idx
			break
		}
	}

	if foundIdx == -1 {
		return fmt.Errorf("device ID '%s' not found", id)
	}

	if srv, ok := m.services[id]; ok {
		_ = srv.Close()
		delete(m.services, id)
	}

	m.devices = append(m.devices[:foundIdx], m.devices[foundIdx+1:]...)
	return m.saveLocked()
}

// GetStatuses queries all registered devices in parallel for live info
func (m *DeviceManager) GetStatuses(ctx context.Context) []DeviceStatus {
	m.mu.RLock()
	devices := make([]DeviceItem, len(m.devices))
	copy(devices, m.devices)
	m.mu.RUnlock()

	statuses := make([]DeviceStatus, len(devices))
	var wg sync.WaitGroup

	for i, dev := range devices {
		wg.Add(1)
		go func(idx int, d DeviceItem) {
			defer wg.Done()

			status := DeviceStatus{
				ID:     d.ID,
				Name:   d.Name,
				IP:     d.IP,
				Port:   d.Port,
				Online: false,
			}

			m.mu.RLock()
			srv := m.services[d.ID]
			m.mu.RUnlock()

			if srv == nil {
				status.Error = "Service uninitialized"
				statuses[idx] = status
				return
			}

			checkCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
			defer cancel()

			info, err := srv.Ping(checkCtx)
			if err != nil {
				status.Online = false
				status.Error = err.Error()
			} else {
				status.Online = true
				status.Model = info.DeviceName
				if status.Model == "" {
					status.Model = info.Model
				}
				status.SerialNumber = info.SerialNumber
				status.Firmware = info.Firmware
				status.Platform = info.Platform

				// Count users
				if users, uErr := srv.GetAllUsers(checkCtx); uErr == nil {
					status.UserCount = len(users)
				}
			}

			statuses[idx] = status
		}(i, dev)
	}

	wg.Wait()
	return statuses
}

// BroadcastUser pushes a user profile to ALL registered devices
func (m *DeviceManager) BroadcastUser(ctx context.Context, u models.User) map[string]error {
	m.mu.RLock()
	devices := make([]DeviceItem, len(m.devices))
	copy(devices, m.devices)
	m.mu.RUnlock()

	results := make(map[string]error)
	var mu sync.Mutex
	var wg sync.WaitGroup

	for _, dev := range devices {
		wg.Add(1)
		go func(d DeviceItem) {
			defer wg.Done()
			m.mu.RLock()
			srv := m.services[d.ID]
			m.mu.RUnlock()

			if srv == nil {
				mu.Lock()
				results[d.ID] = errors.New("service uninitialized")
				mu.Unlock()
				return
			}

			opCtx, cancel := context.WithTimeout(ctx, 6*time.Second)
			defer cancel()

			err := srv.CreateOrUpdateUser(opCtx, u)
			mu.Lock()
			results[d.ID] = err
			mu.Unlock()
		}(dev)
	}

	wg.Wait()
	return results
}

func (m *DeviceManager) Close() {
	m.mu.Lock()
	defer m.mu.Unlock()

	for _, s := range m.services {
		_ = s.Close()
	}
}
