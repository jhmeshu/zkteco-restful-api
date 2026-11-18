package worker

import (
	"context"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/jhmeshu/zkteco-restful-api/internal/config"
	"github.com/jhmeshu/zkteco-restful-api/internal/devicemanager"
	"github.com/jhmeshu/zkteco-restful-api/internal/zkservice"
)

type MultiLiveCaptureWorker struct {
	cfg        *config.Config
	manager    *devicemanager.DeviceManager
	httpClient *http.Client
	running    map[string]context.CancelFunc
	mu         sync.Mutex
}

func NewLiveCaptureWorker(cfg *config.Config, manager *devicemanager.DeviceManager) *MultiLiveCaptureWorker {
	return &MultiLiveCaptureWorker{
		cfg:        cfg,
		manager:    manager,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		running:    make(map[string]context.CancelFunc),
	}
}

// Start supervises live capture workers for all registered devices
func (w *MultiLiveCaptureWorker) Start(ctx context.Context) {
	log.Println("[LiveCapture] Starting multi-device attendance capture supervisor...")

	ticker := time.NewTicker(10 * time.Second)
	defer ticker.Stop()

	// Initial check
	w.syncDeviceWorkers(ctx)

	for {
		select {
		case <-ctx.Done():
			w.stopAll()
			return
		case <-ticker.C:
			w.syncDeviceWorkers(ctx)
		}
	}
}

func (w *MultiLiveCaptureWorker) syncDeviceWorkers(parentCtx context.Context) {
	w.mu.Lock()
	defer w.mu.Unlock()

	currentDevices := w.manager.GetAll()
	activeIDs := make(map[string]bool)

	for _, d := range currentDevices {
		activeIDs[d.ID] = true
		if _, isRunning := w.running[d.ID]; !isRunning {
			// Start worker for new device
			srv, _, err := w.manager.Get(d.ID)
			if err != nil || srv == nil {
				continue
			}

			devCtx, cancel := context.WithCancel(parentCtx)
			w.running[d.ID] = cancel
			go w.runDeviceCapture(devCtx, d, srv)
		}
	}

	// Cancel workers for deleted devices
	for id, cancel := range w.running {
		if !activeIDs[id] {
			cancel()
			delete(w.running, id)
		}
	}
}

func (w *MultiLiveCaptureWorker) runDeviceCapture(ctx context.Context, dev devicemanager.DeviceItem, srv *zkservice.ZkService) {
	log.Printf("[LiveCapture][%s] Starting punch listener for %s (%s)...", dev.ID, dev.Name, dev.IP)

	for {
		select {
		case <-ctx.Done():
			log.Printf("[LiveCapture][%s] Stopped listener", dev.ID)
			return
		default:
		}

		sub, err := srv.SubscribeEvents(ctx)
		if err != nil {
			log.Printf("[LiveCapture][%s] Connect error (%s): %v. Retrying in 6s...", dev.ID, dev.IP, err)
			select {
			case <-time.After(6 * time.Second):
				continue
			case <-ctx.Done():
				return
			}
		}

		log.Printf("[LiveCapture][%s] Connected & subscribed to real-time events on %s", dev.ID, dev.Name)
		eventChan := sub.Events()
		active := true

		for active {
			select {
			case <-ctx.Done():
				_ = sub.Close()
				return
			case event, ok := <-eventChan:
				if !ok {
					log.Printf("[LiveCapture][%s] Event stream disconnected (%s). Reconnecting...", dev.ID, dev.IP)
					active = false
					break
				}

				userID := ""
				if event.Attendance != nil && event.Attendance.UserID != "" {
					userID = event.Attendance.UserID
				} else if event.UserID != "" {
					userID = event.UserID
				}

				if userID != "" {
					log.Printf("[LiveCapture][%s] Punch detected: User %s on %s", dev.ID, userID, dev.Name)
					go w.sendAttendanceWebhook(dev, userID)
				}
			}
		}

		_ = sub.Close()
		time.Sleep(3 * time.Second)
	}
}

func (w *MultiLiveCaptureWorker) sendAttendanceWebhook(dev devicemanager.DeviceItem, memberID string) {
	if w.cfg.BackendURL == "" {
		return
	}

	backendURL := strings.TrimRight(w.cfg.BackendURL, "/") + "/check-in"
	formData := url.Values{
		"member_id":   {memberID},
		"device_id":   {dev.ID},
		"device_name": {dev.Name},
		"device_ip":   {dev.IP},
	}

	resp, err := w.httpClient.PostForm(backendURL, formData)
	if err != nil {
		log.Printf("[LiveCapture][%s] Webhook error for member %s to %s: %v", dev.ID, memberID, backendURL, err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		log.Printf("[LiveCapture][%s] Webhook delivered for member %s (Status %d)", dev.ID, memberID, resp.StatusCode)
	}
}

func (w *MultiLiveCaptureWorker) stopAll() {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, cancel := range w.running {
		cancel()
	}
	w.running = make(map[string]context.CancelFunc)
}
