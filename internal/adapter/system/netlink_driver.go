package system

import (
    "bandwidth_controller_backend/internal/core/domain"
    "bandwidth_controller_backend/internal/core/port"
    "context"
    "fmt"
    "os"
    "os/exec"
    "strconv"
    "strings"
    "time"
    "log"
)

type LinuxDriver struct{}

func NewLinuxDriver() port.NetworkDriver {
    return &LinuxDriver{}
}

// --- TBF/Simple Methods (Your existing, working TBF logic) ---

func (l *LinuxDriver) ApplyShaping(ctx context.Context, rule domain.QoSRule) error {
    // 1. Reset existing rules (Crucial: removes HTB if it was active)
    _ = l.ResetShaping(ctx, rule.Interface)

    // ... (TBF command construction and execution remains the same) ...
    args := []string{
        "qdisc", "add", "dev", rule.Interface, 
        "root", "tbf", 
        "rate", rule.Bandwidth, // NOTE: Changed from RateLimit to Bandwidth based on your domain
        "burst", "10k", 
        "latency", rule.Latency,
    }

    cmd := exec.CommandContext(ctx, "tc", args...)
    output, err := cmd.CombinedOutput()
    
    if err != nil {
        log.Printf("TBF Command failed! Command: tc %v\nStderr Output: %s", args, string(output))
        return fmt.Errorf("tc command failed (exit code %d): %s", cmd.ProcessState.ExitCode(), string(output))
    }
    
    log.Printf("QoS rule successfully applied (TBF mode) on %s: Rate=%s", rule.Interface, rule.Bandwidth)
    return nil
}

func (l *LinuxDriver) GetStatistics(ctx context.Context, iface string) (*domain.TrafficStat, error) {
    // ... (Your existing file reading logic remains the same) ...
    rxPath := fmt.Sprintf("/sys/class/net/%s/statistics/rx_bytes", iface)
    txPath := fmt.Sprintf("/sys/class/net/%s/statistics/tx_bytes", iface)

    rx, err := readStatFile(rxPath)
    if err != nil {
        return nil, err
    }
    
    tx, err := readStatFile(txPath)
    if err != nil {
        return nil, err
    }

    // NOTE: The monitor loop expects domain.TrafficStat
    return &domain.TrafficStat{
        Interface: iface,
        RxBytes:   rx,
        TxBytes:   tx,
        Timestamp: time.Now(),
    }, nil
}

// --- HTB/Complex Methods (New Implementations) ---

// SetupHTBStructure sets up the core HTB QDisc and the 1:1 root class.
func (l *LinuxDriver) SetupHTBStructure(ctx context.Context, iface string, totalBandwidth string) error {
    _ = l.ResetShaping(ctx, iface) // Reset existing rules first

    // 1. Add the main HTB QDisc (root, handle 1:)
    argsQdisc := []string{"qdisc", "add", "dev", iface, "root", "handle", "1:", "htb", "default", "1"}
    if output, err := exec.CommandContext(ctx, "tc", argsQdisc...).CombinedOutput(); err != nil {
        return fmt.Errorf("htb qdisc setup failed: %s", string(output))
    }
    
    // 2. Add the root class (1:1) defining the total capacity
    // This class is where the global rate limit is enforced.
    argsRootClass := []string{"class", "add", "dev", iface, "parent", "1:", "classid", "1:1", "htb", 
                              "rate", totalBandwidth, "ceil", totalBandwidth}
    if output, err := exec.CommandContext(ctx, "tc", argsRootClass...).CombinedOutput(); err != nil {
        return fmt.Errorf("htb root class setup failed: %s", string(output))
    }
    
    // 3. ADD THE CRUCIAL U32 FILTER: Force ALL IP traffic to use the 1:1 class
    // This overrides kernel drivers that might bypass the standard HTB queuing logic,
    // ensuring the shaping takes effect on all uploads.
    // 'parent 1:' means attach to the main QDisc.
    // 'u32 match ip dst 0.0.0.0/0' means match all outgoing IP destinations.
    // 'flowid 1:1' means send the traffic into the 1:1 class.
    argsFilter := []string{"filter", "add", "dev", iface, "parent", "1:", "protocol", "ip", "prio", "10", 
                           "u32", "match", "ip", "dst", "0.0.0.0/0", "flowid", "1:1"}
    
    if output, err := exec.CommandContext(ctx, "tc", argsFilter...).CombinedOutput(); err != nil {
        // Log this as an error, as this is the likely point of failure for shaping.
        log.Printf("ERROR: Failed to add U32 default filter, shaping may fail: %s", string(output))
        return fmt.Errorf("u32 filter failed: %s", string(output))
    }
    
    log.Printf("HTB structure set up on %s with global capacity: %s (Filter applied to 1:1)", iface, totalBandwidth)
    return nil
}

// ApplyGlobalShaping modifies the rate of the HTB 1:1 root class.
func (l *LinuxDriver) ApplyGlobalShaping(ctx context.Context, rule domain.QoSRule) error {
    // Modify the root class (1:1) rate—this is the new global limit
    args := []string{"class", "change", "dev", rule.Interface, "parent", "1:", 
                     "classid", "1:1", "htb", "rate", rule.Bandwidth, "ceil", rule.Bandwidth}
    
    cmd := exec.CommandContext(ctx, "tc", args...)
    if output, err := cmd.CombinedOutput(); err != nil {
        log.Printf("HTB Global Rate change failed. Stderr: %s", string(output))
        return fmt.Errorf("HTB Global Rate change failed: %s", string(output))
    }
    
    log.Printf("Global HTB rate successfully set on %s: Rate=%s", rule.Interface, rule.Bandwidth)
    return nil
}

// Placeholder for future HTB methods
func (l *LinuxDriver) ApplyDevicePolicy(ctx context.Context, policy domain.DevicePolicy) error {
    return fmt.Errorf("ApplyDevicePolicy is unimplemented")
}
func (l *LinuxDriver) RemoveDevicePolicy(ctx context.Context, policy domain.DevicePolicy) error {
    return fmt.Errorf("RemoveDevicePolicy is unimplemented")
}
func (l *LinuxDriver) GetDeviceStatistics(ctx context.Context, iface string) ([]*domain.DeviceStat, error) {
    return nil, fmt.Errorf("GetDeviceStatistics is unimplemented")
}
func (l *LinuxDriver) GetGlobalStatistics(ctx context.Context, iface string) (*domain.TrafficStat, error) {
    // In HTB mode, global stats can still be read from the file system.
    return l.GetStatistics(ctx, iface)
}

func (l *LinuxDriver) DisableOffloading(ctx context.Context, iface string) error {
    log.Printf("Attempting to disable TSO/GSO/GRO/LRO offloading on %s...", iface)
    
    // Command: ethtool -K <iface> tso off gso off gro off lro off
    // We target both transmit (TSO, GSO) and receive (GRO, LRO) offloads.
    args := []string{"-K", iface, "tso", "off", "gso", "off", "gro", "off", "lro", "off"}
    
    cmd := exec.CommandContext(ctx, "ethtool", args...)
    output, err := cmd.CombinedOutput()
    
    // Handle the output. We expect an error if the feature is not supported
    // (which is common, e.g., on wireless adapters), so we look for specific warning text.
    outputStr := strings.ToLower(string(output))
    
    if err != nil {
        if strings.Contains(outputStr, "operation not supported") || strings.Contains(outputStr, "no such device") {
            log.Printf("Warning: Offloading control failed on %s (Operation not supported/Interface error).", iface)
        } else {
            // Log a more serious warning if the failure is unexpected
            log.Printf("Error during offloading control on %s: %s", iface, outputStr)
        }
    } else {
        log.Println("TSO/GSO/GRO/LRO offloading successfully requested/disabled.")
    }
    
    // We return nil here to allow the application to proceed even if ethtool failed 
    // due to lack of support, as we cannot hard-fail the app over an optional kernel feature.
    return nil
}
// --- Cleanup ---

func (l *LinuxDriver) ResetShaping(ctx context.Context, iface string) error {
    // 1. CRITICAL STEP: Disable offloading before doing any tc operations
    l.DisableOffloading(ctx, iface) 

    // 2. Try to remove HTB/TBF on the real interface (wlo1 or eth0)
    exec.CommandContext(ctx, "tc", "qdisc", "del", "dev", iface, "root").Run()
    exec.CommandContext(ctx, "tc", "qdi sc", "del", "dev", iface, "clsact").Run()
    
    // ... (rest of the TeardownIFBDevice/IFB logic, if you kept it) ...
    
    return nil
}
// readStatFile remains the same
func readStatFile(path string) (uint64, error) {
    data, err := os.ReadFile(path)
    if err != nil {
        return 0, fmt.Errorf("failed to read file %s: %w", path, err)
    }
    // Trim whitespace (newline) and parse the integer
    valueStr := strings.TrimSpace(string(data))
    value, err := strconv.ParseUint(valueStr, 10, 64)
    if err != nil {
        return 0, fmt.Errorf("failed to parse value '%s': %w", valueStr, err)
    }
    return value, nil
}