# ZKTeco RESTful API & Live Attendance Service

A lightweight, high-performance, **multiplatform (Windows, Linux, macOS)** REST API gateway and real-time attendance webhook worker for ZKTeco biometric devices. Written in **Go (Golang)** using the **Gin** web framework.

---

## Features

- **Multiplatform & Standalone**: Compiles into a single self-contained executable (`.exe` on Windows, or static binary on Linux/macOS) with **zero external dependencies** (no Python, no .NET, no DLLs required).
- **Unified Single Process**: REST API and live biometric attendance capture run concurrently in a single process without socket collisions.
- **Auto-Reconnection**: Resilient background reconnect loop when the hardware or network disconnects.
- **Full Biometric & User Management**: Remote user creation, deletion, and fingerprint template capture/enrollment.
- **Instant Webhooks**: Pushes punch-clock logs in real time to your external application (`POST /check-in`).

---

## Quick Start

### 1. Configuration
Copy `.env.example` to `.env` and set your device IP and backend webhook URL:
```bash
cp .env.example .env
```

```env
DEVICE_IP=192.168.3.18
DEVICE_PORT=4370
DEVICE_PASSWORD=0
BACKEND_URL=http://localhost:8000/api
PORT=4000
DEBUG=false
```

### 2. Run Directly
```bash
go run ./cmd/api
```

---

## Building Executables

### Build for Windows (from any OS):
```bash
make build-windows
# Or directly with Go:
GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o bin/zkteco-api.exe ./cmd/api
```
This generates `bin/zkteco-api.exe`, which you can copy to any Windows machine. Just place a `.env` file next to it and double-click or run from CMD/PowerShell.

### Build for Linux:
```bash
make build-linux
```

### Build for macOS:
```bash
make build-mac
```

---

## Running as a Background Windows Service

To have the application start automatically when the Windows computer turns on (without keeping a terminal window open):

1. Download [NSSM (Non-Sucking Service Manager)](https://nssm.cc/download).
2. Open PowerShell or Command Prompt as **Administrator**:
   ```cmd
   nssm install ZKTecoService "C:\path\to\zkteco-api.exe"
   nssm set ZKTecoService AppDirectory "C:\path\to"
   nssm start ZKTecoService
   ```
3. To view status or stop the service:
   ```cmd
   nssm status ZKTecoService
   nssm stop ZKTecoService
   ```

---

## Docker Deployment

To build and run the multi-stage lightweight Docker container:
```bash
docker build -t zkteco-api .
docker run -d --name zkteco-api -p 4000:4000 --env-file .env --restart always zkteco-api
```

---

## API Endpoints

### 1. Health & Device Status
- **`GET /health`**: Healthcheck endpoint.
- **`GET /device/capture`**: Tests connection to the ZKTeco device and returns firmware, platform, and serial number.

### 2. User Management
- **`POST /user`**: Create or update user on the device.
  ```json
  {
    "user_id": 1,
    "user_data": {
      "name": "John Doe",
      "privilege": 0,
      "password": "",
      "group_id": 0,
      "card": 0
    }
  }
  ```
- **`GET /users`**: Get list of all enrolled users.
- **`DELETE /user/:user_id`**: Delete a user from the device.

### 3. Fingerprint Templates
- **`POST /user/:user_id/fingerprint`**: Put device in enrollment mode for a specific finger index (`temp_id` 0–9).
  ```json
  {
    "temp_id": 0
  }
  ```
- **`GET /user/:user_id/fingerprint/:temp_id`**: Retrieve base64-encoded fingerprint template.
- **`DELETE /user/:user_id/fingerprint/:temp_id`**: Delete fingerprint template.

### 4. Biometric Enrollment
- **`POST /devices/:device_id/user/:user_id/fingerprint`**: Trigger fingerprint scanning on physical device.
- **`POST /devices/:device_id/user/:user_id/face`**: Trigger facial recognition camera scanning on terminal screen.

### 5. Device Control & Maintenance Commands
- **`POST /devices/:device_id/reboot`** (or `POST /device/reboot`): Reboot the physical terminal.
- **`POST /devices/:device_id/clear-attendance`** (or `POST /device/clear-attendance`): **Data Clean** — erases all stored attendance check-in logs from device memory.
- **`POST /devices/:device_id/refresh-cache`** (or `POST /device/refresh-cache`): **Cache Clean** — reloads internal device memory, users, and biometric templates.
- **`POST /devices/:device_id/wipe`** (or `POST /device/wipe`): **Data Wipe** — permanently erases all users, fingerprints, faces, and punches.
- **`POST /devices/:device_id/command`**: Unified command runner:
  ```json
  {
    "command": "reboot" // "clean_punches" | "clean_cache" | "data_wipe"
  }
  ```

### 6. Real-time Attendance Webhook
Whenever a user punches on the physical device, the service automatically sends a POST request to:
```
POST {BACKEND_URL}/check-in
Content-Type: application/x-www-form-urlencoded

member_id=1
```
