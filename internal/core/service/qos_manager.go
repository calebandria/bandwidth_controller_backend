package service

import (
    "context"
    "errors" // 🚨 ADDED: Required for errors.New
    "log"
    "time"

    "bandwidth_controller_backend/internal/core/domain"
    "bandwidth_controller_backend/internal/core/port"
)

const (
    ModeGlobalTBF = "TBF"
    ModeGlobalHTB = "HTB"
)

// QoSManager manages the application's business logic for traffic control.
type QoSManager struct {
    driver      port.NetworkDriver
    statsStream chan domain.TrafficStat // 🚨 CORRECTED: Used TrafficStat which aligns with your driver GetStatistics
    threshold   uint64 
    iface       string 
    activeMode  string // 🚨 ADDED: State tracking for TBF vs. HTB
}

func NewQoSManager(driver port.NetworkDriver) *QoSManager {
    mgr := &QoSManager{
        driver:      driver,
        // Using domain.TrafficStat for consistency with GetStatistics implementation
        statsStream: make(chan domain.TrafficStat, 100), 
        threshold:   100000000,                      
        iface:       "wlo1",
        activeMode:  ModeGlobalTBF, // 🚨 Set default mode
    }
    
    go mgr.runMonitorLoop()
    return mgr
}

// ---------------------------------------------------------------------
// Service Implementation
// ---------------------------------------------------------------------

// SetupGlobalQoS establishes the HTB structure on the interface and switches mode.
func (s *QoSManager) SetupGlobalQoS(ctx context.Context, iface string, maxRate string) error {
    if err := s.driver.SetupHTBStructure(ctx, iface, maxRate); err != nil {
        return err
    }
    s.activeMode = ModeGlobalHTB // Switch to HTB mode
    return nil
}

// UpdateGlobalLimit modifies the HTB 1:1 root class limit.
func (s *QoSManager) UpdateGlobalLimit(ctx context.Context, rule domain.QoSRule) error {
    if s.activeMode != ModeGlobalHTB {
        return errors.New("HTB mode is not active. Please call /qos/setup first.")
    }
    // Correct call: Uses the HTB-specific driver method
    return s.driver.ApplyGlobalShaping(ctx, rule)
}

// SetSimpleGlobalLimit implements the TBF logic.
func (s *QoSManager) SetSimpleGlobalLimit(ctx context.Context, rule domain.QoSRule) error {
    if s.activeMode == ModeGlobalHTB {
        return errors.New("cannot apply TBF limit while HTB is active. Reset first.")
    }
    
    // Switch to TBF mode and apply TBF shaping
    s.activeMode = ModeGlobalTBF
    // Correct call: Uses the TBF-specific driver method
    return s.driver.ApplyShaping(ctx, rule) 
}

// GetLiveStats is updated to stream the correct type.
func (s *QoSManager) GetLiveStats() <-chan domain.TrafficStat {
    return s.statsStream
}

func (s *QoSManager) runMonitorLoop() {
    ticker := time.NewTicker(1 * time.Second)
    ctx := context.Background()

    for range ticker.C {
        // Use the mode to decide which stats to retrieve (simple for now)
        stat, err := s.driver.GetStatistics(ctx, s.iface) 
        if err != nil {
            log.Printf("Error fetching global stats: %v", err)
            continue
        }

        // ... (WebSocket push remains the same) ...
        select {
        case s.statsStream <- *stat:
        default:
        }

        // 2. LOGIC: Auto-throttle only applies if in the correct mode
        if s.activeMode == ModeGlobalHTB && stat.RxBytes > s.threshold {
            log.Printf("Threshold exceeded! Auto-throttling %s to 1mbit.", s.iface)
            
            // Auto-throttle calls the HTB global update method
            err := s.driver.ApplyGlobalShaping(ctx, domain.QoSRule{
                Interface: s.iface,
                Bandwidth: "1mbit", 
                Latency:   "100ms",
                Enabled:   true,
            })
            if err != nil {
                log.Printf("Auto-throttle failure: %v", err)
            }
        }
    }
}