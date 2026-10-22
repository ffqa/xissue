package server

import (
	"net/http"
	"os"
	"runtime"
	"time"
)

// SystemStatus is the bottom toolbar feed. Values are sampled on request.
type SystemStatus struct {
	Version       string       `json:"version"`
	GoVersion     string       `json:"goVersion"`
	OS            string       `json:"os"`
	Arch          string       `json:"arch"`
	PID           int          `json:"pid"`
	UptimeSeconds int64        `json:"uptimeSeconds"`
	Goroutines    int          `json:"goroutines"`
	AutoUpdate    bool         `json:"autoUpdate"`
	LatestUpdate  string       `json:"latestUpdate,omitempty"`
	Memory        MemoryStatus `json:"memory"`
	CPU           CPUStatus    `json:"cpu"`
	Disk          DiskStatus   `json:"disk"`
	GC            GCStatus     `json:"gc"`
}

type MemoryStatus struct {
	HeapAlloc   uint64 `json:"heapAlloc"`
	HeapInuse   uint64 `json:"heapInuse"`
	HeapObjects uint64 `json:"heapObjects"`
	Sys         uint64 `json:"sys"`
	NextGC      uint64 `json:"nextGC"`
}

type CPUStatus struct {
	ProcessPercent float64 `json:"processPercent"`
	Cores          int     `json:"cores"`
}

type DiskStatus struct {
	Path        string  `json:"path"`
	Total       uint64  `json:"total"`
	Free        uint64  `json:"free"`
	UsedPercent float64 `json:"usedPercent"`
	Error       string  `json:"error,omitempty"`
}

type GCStatus struct {
	LastGC       time.Time `json:"lastGC"`
	NumGC        uint32    `json:"numGC"`
	NextGCBytes  uint64    `json:"nextGCBytes"`
	EstimatedSec float64   `json:"estimatedSec"`
	PauseTotalMs float64   `json:"pauseTotalMs"`
}

func (s *Server) collectSystemStatus() SystemStatus {
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	now := time.Now()

	s.statusMu.Lock()
	cpu := 0.0
	if !s.lastCPUAt.IsZero() {
		if dt := now.Sub(s.lastCPUAt).Seconds(); dt > 0 {
			cpu = (processCPUSeconds() - s.lastCPUSeconds) / dt * 100
			if cpu < 0 {
				cpu = 0
			}
		}
	}
	s.lastCPUSeconds = processCPUSeconds()
	s.lastCPUAt = now

	var allocRate float64
	if !s.lastHeapAt.IsZero() {
		if dt := now.Sub(s.lastHeapAt).Seconds(); dt > 0 && m.HeapAlloc > s.lastHeapAlloc {
			allocRate = float64(m.HeapAlloc-s.lastHeapAlloc) / dt
		}
	}
	s.lastHeapAlloc = m.HeapAlloc
	s.lastHeapAt = now
	s.statusMu.Unlock()

	estimated := 0.0
	if allocRate > 0 && m.NextGC > m.HeapAlloc {
		estimated = float64(m.NextGC-m.HeapAlloc) / allocRate
		if estimated > 3600 {
			estimated = 3600
		}
	}
	total, free, diskErr := diskUsage(s.cfg.DataDir)
	disk := DiskStatus{Path: s.cfg.DataDir, Total: total, Free: free}
	if diskErr != nil {
		disk.Error = diskErr.Error()
	}
	if total > 0 {
		disk.UsedPercent = float64(total-free) / float64(total) * 100
	}
	var lastGC time.Time
	if m.LastGC > 0 {
		lastGC = time.Unix(0, int64(m.LastGC))
	}

	s.updateMu.Lock()
	latest := s.latestUpdate
	s.updateMu.Unlock()

	return SystemStatus{
		Version:       Version,
		GoVersion:     runtime.Version(),
		OS:            runtime.GOOS,
		Arch:          runtime.GOARCH,
		PID:           os.Getpid(),
		UptimeSeconds: int64(time.Since(s.startedAt).Seconds()),
		Goroutines:    runtime.NumGoroutine(),
		AutoUpdate:    s.cfg.AutoUpdateEnabled(),
		LatestUpdate:  latest,
		Memory: MemoryStatus{
			HeapAlloc:   m.HeapAlloc,
			HeapInuse:   m.HeapInuse,
			HeapObjects: m.HeapObjects,
			Sys:         m.Sys,
			NextGC:      m.NextGC,
		},
		CPU:  CPUStatus{ProcessPercent: cpu, Cores: runtime.NumCPU()},
		Disk: disk,
		GC: GCStatus{
			LastGC:       lastGC,
			NumGC:        m.NumGC,
			NextGCBytes:  m.NextGC,
			EstimatedSec: estimated,
			PauseTotalMs: float64(m.PauseTotalNs) / 1e6,
		},
	}
}

func (s *Server) handleSystemStatus(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.collectSystemStatus())
}

// statusLoop samples and fans out status only while a browser is connected.
func (s *Server) statusLoop() {
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-s.stopCh:
			return
		case <-ticker.C:
		}
		if s.hub.ClientCount() == 0 {
			continue
		}
		s.hub.BroadcastStatus(s.collectSystemStatus())
	}
}
