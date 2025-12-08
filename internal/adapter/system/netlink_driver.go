package system

import (
	"bandwidth_controller_backend/internal/core/domain"
	"bandwidth_controller_backend/internal/core/port"
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ActiveIPConfig maps the IP address to its assigned HTB class ID (e.g., 10 for 1:10).
type ActiveIPConfig map[string]uint16

// Global state for traffic rate calculation (per interface)
var trafficState = make(map[string]*domain.TrafficState)
var stateMutex sync.RWMutex

// State specific to IP traffic rate calculation (IP-Interface -> Stats)
var ipTrafficState = make(map[string]*domain.TrafficState)
var ipStateMutex sync.RWMutex

// LinuxDriver implements the NetworkDriver interface using Linux tc and iptables.
type LinuxDriver struct {
	// activeIPs tracks the class ID assigned to an IP to manage HTB classes (local state for QoS)
	activeIPs ActiveIPConfig
	mu          sync.Mutex // Mutex for protecting activeIPs and nextClassID
	nextClassID uint16     // Counter to assign unique class IDs starting from 10
}


func NewLinuxDriver() port.NetworkDriver {
	return &LinuxDriver{
		activeIPs:   make(ActiveIPConfig),
		nextClassID: 10, // Start with 10 (1:1 is root)
	}
}

// applyTcCommand executes a 'tc' command and handles errors.
func applyTcCommand(ctx context.Context, args []string, iface string) error {
	cmd := exec.CommandContext(ctx, "tc", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		errMsg := fmt.Sprintf("Command failed on %s! Command: tc %v\nOutput: %s", iface, args, string(output))
		log.Println("ERROR: TcCommand:", errMsg)
		// We often ignore "File exists" errors when adding filters if they already exist,
		// but for class/qdisc add, we treat it as an error unless it's a reset operation.
		return fmt.Errorf("tc command failed (exit code %d): %s", cmd.ProcessState.ExitCode(), string(output))
	}

	return nil
}

// applyIptablesCommand executes an 'iptables' command and handles errors.
func applyIptablesCommand(ctx context.Context, args []string) error {
	cmd := exec.CommandContext(ctx, "iptables", args...)
	output, err := cmd.CombinedOutput()

	if err != nil {
		errMsg := fmt.Sprintf("Command failed! Command: iptables %v\nOutput: %s", args, string(output))
		log.Println("ERROR: IptablesCommand:", errMsg)
		return fmt.Errorf("iptables command failed: %s", string(output))
	}
	return nil
}

// ExecCommand is a general command executor
func ExecCommand(ctx context.Context, name string, arg ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, arg...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("error executing command: %s, output: %s", err, string(output))
	}
	return string(output), nil
}

// ApplyShaping uses TBF for simple global limits on both interfaces.
func (l *LinuxDriver) ApplyShaping(ctx context.Context, rule domain.QoSRule) error {
	_ = l.ResetShaping(ctx, rule.LanInterface, rule.WanInterface)

	// --- LAN Interface Shaping (Download toward client) ---
	lan_args := []string{
		"qdisc", "add", "dev", rule.LanInterface,
		"root", "tbf",
		"rate", rule.Bandwidth,
		"burst", "10k",
		"latency", rule.Latency,
	}

	if err := applyTcCommand(ctx, lan_args, rule.LanInterface); err != nil {
		return fmt.Errorf("lan shaping failed (Download limit): %w", err)
	}
	log.Printf("QoS rule successfully applied (TBF mode) on %s (Download): Rate=%s", rule.LanInterface, rule.Bandwidth)

	// --- WAN Interface Shaping (Upload toward internet) ---
	wan_args := []string{
		"qdisc", "add", "dev", rule.WanInterface,
		"root", "tbf",
		"rate", rule.Bandwidth,
		"burst", "10k",
		"latency", rule.Latency,
	}

	if err := applyTcCommand(ctx, wan_args, rule.WanInterface); err != nil {
		return fmt.Errorf("wan shaping failed (Upload limit): %w", err)
	}
	log.Printf("QoS rule successfully applied (TBF mode) on %s (Upload): Rate=%s", rule.WanInterface, rule.Bandwidth)

	return nil
}

// SetupHTBStructure initializes HTB qdisc and root class 1:1 on both interfaces.
func (l *LinuxDriver) SetupHTBStructure(ctx context.Context, ilan string, iwan string, totalBandwidth string) error {
	_ = l.ResetShaping(ctx, ilan, iwan)

	// Helper function to set up HTB on a single interface
	setupIface := func(iface string) error {
		if iface == "" {
			return nil
		}

		// 1. HTB Setup QDisc (Egress/Root)
		argsQdisc := []string{"qdisc", "add", "dev", iface, "root", "handle", "1:", "htb", "default", "1"}
		if err := applyTcCommand(ctx, argsQdisc, iface); err != nil {
			return fmt.Errorf("htb qdisc setup failed on %s: %w", iface, err)
		}

		// 2. HTB Root Class 1:1
		argsRootClass := []string{"class", "add", "dev", iface, "parent", "1:", "classid", "1:1", "htb",
			"rate", totalBandwidth, "ceil", totalBandwidth}
		if err := applyTcCommand(ctx, argsRootClass, iface); err != nil {
			return fmt.Errorf("htb root class setup failed on %s: %w", iface, err)
		}
		log.Printf("HTB structure set up on %s with global capacity: %s (Root Class 1:1)", iface, totalBandwidth)
		return nil
	}

	// 1. HTB Setup for LAN (Download Shaping)
	if err := setupIface(ilan); err != nil {
		return err
	}

	// 2. HTB Setup for WAN (Upload Shaping)
	if err := setupIface(iwan); err != nil {
		return err
	}

	return nil
}

// ApplyGlobalShaping updates the HTB root class 1:1 on both interfaces.
func (l *LinuxDriver) ApplyGlobalShaping(ctx context.Context, rule domain.QoSRule) error {
	// --- Change rate on LAN Interface (Download) ---
	argsLAN := []string{"class", "change", "dev", rule.LanInterface, "parent", "1:",
		"classid", "1:1", "htb", "rate", rule.Bandwidth, "ceil", rule.Bandwidth}

	if err := applyTcCommand(ctx, argsLAN, rule.LanInterface); err != nil {
		return fmt.Errorf("htb global rate change failed on %s: %w", rule.LanInterface, err)
	}
	log.Printf("Global HTB rate successfully set on %s (Download): Rate=%s", rule.LanInterface, rule.Bandwidth)

	// --- Change rate on WAN Interface (Upload) ---
	argsWAN := []string{"class", "change", "dev", rule.WanInterface, "parent", "1:",
		"classid", "1:1", "htb", "rate", rule.Bandwidth, "ceil", rule.Bandwidth}

	if err := applyTcCommand(ctx, argsWAN, rule.WanInterface); err != nil {
		return fmt.Errorf("htb global rate change failed on %s: %w", rule.WanInterface, err)
	}

	log.Printf("Global HTB rate successfully set on %s (Upload): Rate=%s", rule.WanInterface, rule.Bandwidth)

	return nil
}

// ResetShaping deletes the root qdisc on both interfaces.
func (l *LinuxDriver) ResetShaping(ctx context.Context, ilan string, iwan string) error {
	var firstErr error

	delQdisc := func(iface string) error {
		cmd := exec.CommandContext(ctx, "tc", "qdisc", "del", "dev", iface, "root")
		output, err := cmd.CombinedOutput()

		if err != nil {
			outputStr := string(output)
			// Ignore "No such file or directory" or "Invalid argument" if qdisc doesn't exist
			if strings.Contains(outputStr, "No such file or directory") || strings.Contains(outputStr, "Invalid argument") {
				return nil
			}

			log.Printf("ERROR: Failed to reset shaping on %s. Output: %s", iface, outputStr)
			return fmt.Errorf("failed to delete qdisc on %s: %s", iface, outputStr)
		}
		log.Printf("QDisc reset successful on %s.", iface)
		return nil
	}
    
    // Clear local state tracking when resetting the entire QoS
    l.mu.Lock()
    l.activeIPs = make(ActiveIPConfig)
    l.nextClassID = 10
    l.mu.Unlock()

	if err := delQdisc(ilan); err != nil {
		firstErr = err
	}

	if err := delQdisc(iwan); err != nil {
		if firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

// GetConnectedLANIPs reads the ARP/Neighbor table.
func (l *LinuxDriver) GetConnectedLANIPs(ctx context.Context, ilan string) ([]string, error) {
	output, err := ExecCommand(ctx, "ip", "neighbor", "show", "dev", ilan)
	if err != nil {
		log.Printf("ip neighbor command failed for %s: %v, output: %s", ilan, err, output)
		return nil, fmt.Errorf("impossible de lire la table ARP pour %s: %w", ilan, err)
	}

	var ips []string
	// Regex to find IPv4 or IPv6 addresses
	ipRegex := regexp.MustCompile(`(\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b)|(\b[0-9a-fA-F:]+\b)`)

	lines := strings.Split(output, "\n")

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		// Filter for active states
		if strings.Contains(trimmedLine, "REACHABLE") || strings.Contains(trimmedLine, "STALE") || strings.Contains(trimmedLine, "DELAY") || strings.Contains(trimmedLine, "PERMANENT") {
			matches := ipRegex.FindAllString(trimmedLine, -1)

			for _, match := range matches {
				// Ignore IPv6 link-local addresses
				if !strings.HasPrefix(match, "fe80:") && strings.Contains(match, ".") { // Only grab IPv4 for simplicity
					ips = append(ips, match)
					break
				}
			}
		}
	}

	return ips, nil
}

// GetInstantaneousNetDevStats reads raw byte counters from /proc/net/dev.
// NOTE: This method is generally used for global interface stats, not class-specific stats.
func (l *LinuxDriver) GetInstantaneousNetDevStats(iface string) (domain.NetDevStats, error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return domain.NetDevStats{}, fmt.Errorf("impossible de lire /proc/net/dev: %w", err)
	}

	lines := strings.Split(string(data), "\n")

	for _, line := range lines {
		trimmedLine := strings.TrimSpace(line)

		if strings.HasPrefix(trimmedLine, iface+":") {
			fieldsStr := strings.TrimPrefix(trimmedLine, iface+":")
			fields := strings.Fields(fieldsStr)

			// Check for the minimum required fields (Rx bytes is field 0, Tx bytes is field 8)
			if len(fields) >= 10 {
				rxBytes, errRx := strconv.ParseUint(fields[0], 10, 64)
				txBytes, errTx := strconv.ParseUint(fields[8], 10, 64)

				if errRx != nil || errTx != nil {
					return domain.NetDevStats{}, fmt.Errorf("erreur de parsing des octets pour %s: %v, %v", iface, errRx, errTx)
				}

				return domain.NetDevStats{
					RxBytes: rxBytes,
					TxBytes: txBytes,
				}, nil
			}
			break
		}
	}

	return domain.NetDevStats{}, fmt.Errorf("interface %s non trouvée dans /proc/net/dev", iface)
}

// CalculateRateMbps uses the global trafficState to calculate the rate in Mbps.
func (l *LinuxDriver) CalculateRateMbps(iface string, currentStats domain.NetDevStats) (txRateMbps float64, rxRateMbps float64, err error) {
	stateMutex.Lock()
	state, exists := trafficState[iface]
	if !exists {
		// Initialization of state for the interface
		trafficState[iface] = &domain.TrafficState{
			LastStats: currentStats,
			LastTime:  time.Now(),
			Mu:        sync.Mutex{}, // Initialize mutex for thread safety on the state object
		}
		stateMutex.Unlock()
		// No rate to calculate on the first read
		return 0, 0, nil
	}
	stateMutex.Unlock()

	state.Mu.Lock()
	defer state.Mu.Unlock()

	// Calculate time difference
	currentTime := time.Now()
	timeDiff := currentTime.Sub(state.LastTime).Seconds()

	if timeDiff == 0 {
		return 0, 0, nil
	}

	// Calculate byte difference (handle counter wrap-around by resetting to 0 if negative)
	txDiff := int64(currentStats.TxBytes) - int64(state.LastStats.TxBytes)
	rxDiff := int64(currentStats.RxBytes) - int64(state.LastStats.RxBytes)

	if txDiff < 0 {
		txDiff = 0
	}
	if rxDiff < 0 {
		rxDiff = 0
	}

	// Calculate rate in Bytes/sec (Bps)
	txRateBps := float64(txDiff) / timeDiff
	rxRateBps := float64(rxDiff) / timeDiff

	// Convert Bytes/sec to Megabits/sec (Bps * 8 bits/Byte / (1024*1024) bits/Megabit)
	const factor = 8.0 / 1024.0 / 1024.0

	txRateMbps = txRateBps * factor
	rxRateMbps = rxRateBps * factor

	// Update state for the next read
	state.LastStats = currentStats
	state.LastTime = currentTime

	return txRateMbps, rxRateMbps, nil
}

// GetActiveIPs returns a copy of the currently active IP to ClassID mapping.
// This is used by the QoSManager to know which HTB classes to poll for real-time statistics.
func (l *LinuxDriver) GetActiveIPs() map[string]uint16 {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Return a copy to prevent external modification of the driver's internal state
	activeCopy := make(map[string]uint16, len(l.activeIPs))
	for ip, classID := range l.activeIPs {
		activeCopy[ip] = classID
	}
	return activeCopy
}

// AddIPRateLimit creates HTB classes, iptables marks, and tc filters for bi-directional shaping.
func (l *LinuxDriver) AddIPRateLimit(ctx context.Context, ip string, rule domain.QoSRule) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Check if IP already exists (for idempotent calls or monitoring setup)
	if _, exists := l.activeIPs[ip]; exists {
		// Log a warning or proceed if update logic is complex. For simplicity, we just log and reuse.
		log.Printf("IP %s already has an HTB class assigned. Re-using or attempting update.", ip)
		// For the monitoring setup, the QoSManager handles removal/re-addition, so we proceed if the class is not present
		// but if it is present, we should handle the update instead of re-creating everything.
		// Given the QoSManager logic, we assume AddIPRateLimit is called only if the IP is NOT in activeIPLimits 
		// (or if the QoSManager explicitly requested removal before adding).
	}
	
	// Check if this is a known IP being re-added after manual removal. If so, don't increment.
	// For robustness, we stick to sequential ID for simple class creation.
	classID := l.nextClassID
	l.nextClassID++

	l.activeIPs[ip] = classID
	log.Printf("Assigning HTB Class/Mark ID 1:%d to IP %s", classID, ip)

	markID := fmt.Sprintf("%d", classID)

	// =========================================================================
	// PART 1: WAN (Upload Shaping) - Trafic sortant du LAN vers Internet
	// Appliqué sur wanIface, marqué par IP Source (-s)
	// =========================================================================

	// 1. Setup HTB Class on WAN
	wanClassArgs := []string{"class", "add", "dev", rule.WanInterface, "parent", "1:1",
		"classid", fmt.Sprintf("1:%s", markID), "htb",
		"rate", rule.Bandwidth, "ceil", rule.Bandwidth}
	if err := applyTcCommand(ctx, wanClassArgs, rule.WanInterface); err != nil {
		delete(l.activeIPs, ip)
		return fmt.Errorf("failed to add HTB class 1:%s on %s (WAN): %w", markID, rule.WanInterface, err)
	}

	// 2. Setup IPTABLES MARK on WAN (POSTROUTING - IP Source)
	iptablesTxArgs := []string{"-t", "mangle", "-A", "POSTROUTING", "-o", rule.WanInterface,
		"-s", ip, "-j", "MARK", "--set-mark", markID}
	if err := applyIptablesCommand(ctx, iptablesTxArgs); err != nil {
		l.RemoveIPRateLimit(ctx, ip, rule)
		return fmt.Errorf("failed to add iptables Tx mark for %s on %s: %w", ip, rule.WanInterface, err)
	}

	// 3. Setup TC Filter on WAN
	filterTxArgs := []string{"filter", "add", "dev", rule.WanInterface, "parent", "1:", "protocol", "ip",
		"prio", "1", "handle", markID, "fw", "classid", fmt.Sprintf("1:%s", markID)}
	if err := applyTcCommand(ctx, filterTxArgs, rule.WanInterface); err != nil {
		l.RemoveIPRateLimit(ctx, ip, rule)
		return fmt.Errorf("failed to add tc Tx filter for %s on %s: %w", ip, rule.WanInterface, err)
	}

	// =========================================================================
	// PART 2: LAN (Download Shaping) - Trafic venant d'Internet vers le LAN
	// Appliqué sur lanIface, marqué par IP Destination (-d)
	// =========================================================================

	// 4. Setup HTB Class on LAN (re-using the same classID)
	lanClassArgs := []string{"class", "add", "dev", rule.LanInterface, "parent", "1:1",
		"classid", fmt.Sprintf("1:%s", markID), "htb",
		"rate", rule.Bandwidth, "ceil", rule.Bandwidth}
	if err := applyTcCommand(ctx, lanClassArgs, rule.LanInterface); err != nil {
		l.RemoveIPRateLimit(ctx, ip, rule)
		return fmt.Errorf("failed to add HTB class 1:%s on %s (LAN): %w", markID, rule.LanInterface, err)
	}

	// 5. Setup IPTABLES MARK on LAN (POSTROUTING - IP Destination)
	// Le trafic de Destination vers le LAN (-d) qui sort de l'interface LAN (-o) est marqué.
	iptablesRxArgs := []string{"-t", "mangle", "-A", "POSTROUTING", "-o", rule.LanInterface,
		"-d", ip, "-j", "MARK", "--set-mark", markID}
	if err := applyIptablesCommand(ctx, iptablesRxArgs); err != nil {
		l.RemoveIPRateLimit(ctx, ip, rule)
		return fmt.Errorf("failed to add iptables Rx mark for %s on %s: %w", ip, rule.LanInterface, err)
	}

	// 6. Setup TC Filter on LAN
	filterRxArgs := []string{"filter", "add", "dev", rule.LanInterface, "parent", "1:", "protocol", "ip",
		"prio", "1", "handle", markID, "fw", "classid", fmt.Sprintf("1:%s", markID)}
	if err := applyTcCommand(ctx, filterRxArgs, rule.LanInterface); err != nil {
		l.RemoveIPRateLimit(ctx, ip, rule)
		return fmt.Errorf("failed to add tc Rx filter for %s on %s: %w", ip, rule.LanInterface, err)
	}

	log.Printf("IP QoS successfully applied bi-directionally to %s: Rate=%s, ClassID=1:%s", ip, rule.Bandwidth, markID)

	return nil
}

// RemoveIPRateLimit removes the HTB class, iptables mark, and tc filter for a specific IP.
func (l *LinuxDriver) RemoveIPRateLimit(ctx context.Context, ip string, rule domain.QoSRule) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	classID, ok := l.activeIPs[ip]
	if !ok {
		return fmt.Errorf("no HTB class found for IP %s", ip)
	}

	markID := fmt.Sprintf("%d", classID)

	// --- PART 1: WAN (Upload) Cleanup ---

	// 1. Delete TC Filter on WAN
	filterTxArgs := []string{"filter", "del", "dev", rule.WanInterface, "parent", "1:", "protocol", "ip", "prio", "1", "handle", markID, "fw"}
	if err := applyTcCommand(ctx, filterTxArgs, rule.WanInterface); err != nil {
		log.Printf("Warning: Failed to delete tc Tx filter for %s on %s: %v", ip, rule.WanInterface, err)
	}

	// 2. Delete HTB Class on WAN
	wanClassArgs := []string{"class", "del", "dev", rule.WanInterface, "parent", "1:1", "classid", fmt.Sprintf("1:%s", markID)}
	if err := applyTcCommand(ctx, wanClassArgs, rule.WanInterface); err != nil {
		log.Printf("Warning: Failed to delete HTB class 1:%s on %s: %v", markID, rule.WanInterface, err)
	}

	// 3. Delete IPTABLES MARK on WAN (POSTROUTING, -D to delete, -s source IP)
	iptablesTxArgs := []string{"-t", "mangle", "-D", "POSTROUTING", "-o", rule.WanInterface,
		"-s", ip, "-j", "MARK", "--set-mark", markID}
	if err := applyIptablesCommand(ctx, iptablesTxArgs); err != nil {
		log.Printf("Warning: Failed to delete iptables Tx mark for %s: %v", ip, err)
	}

	// --- PART 2: LAN (Download) Cleanup ---

	// 4. Delete TC Filter on LAN
	filterRxArgs := []string{"filter", "del", "dev", rule.LanInterface, "parent", "1:", "protocol", "ip", "prio", "1", "handle", markID, "fw"}
	if err := applyTcCommand(ctx, filterRxArgs, rule.LanInterface); err != nil {
		log.Printf("Warning: Failed to delete tc Rx filter for %s on %s: %v", ip, rule.LanInterface, err)
	}

	// 5. Delete HTB Class on LAN
	lanClassArgs := []string{"class", "del", "dev", rule.LanInterface, "parent", "1:1", "classid", fmt.Sprintf("1:%s", markID)}
	if err := applyTcCommand(ctx, lanClassArgs, rule.LanInterface); err != nil {
		log.Printf("Warning: Failed to delete HTB class 1:%s on %s: %v", markID, rule.LanInterface, err)
	}

	// 6. Delete IPTABLES MARK on LAN (POSTROUTING, -D to delete, -d destination IP)
	iptablesRxArgs := []string{"-t", "mangle", "-D", "POSTROUTING", "-o", rule.LanInterface,
		"-d", ip, "-j", "MARK", "--set-mark", markID}
	if err := applyIptablesCommand(ctx, iptablesRxArgs); err != nil {
		log.Printf("Warning: Failed to delete iptables Rx mark for %s: %v", ip, err)
	}

	delete(l.activeIPs, ip)
	log.Printf("IP QoS successfully removed for %s", ip)
	return nil
}

// internal/adapter/system/linux_driver.go
func (l *LinuxDriver) GetInstantaneousClassStats(iface string, classID uint16) (domain.NetDevStats, error) {
    markID := fmt.Sprintf("1:%d", classID)

    // tc -s class show dev eth0 classid 1:10
    args := []string{"-s", "class", "show", "dev", iface, "classid", markID}
    // Remplace ExecCommand par exec.Command car l'implémentation de ExecCommand est inconnue et peut être simplifiée.
    outputBytes, err := exec.Command("tc", args...).Output() 
    output := string(outputBytes) // Convertir la sortie en chaîne

    if err != nil {
        // Si la classe n'existe pas, on retourne 0, c'est normal si la règle vient d'être supprimée.
        // Utiliser l'erreur ou la sortie pour détecter "Object not found"
        if strings.Contains(output, "Object not found") || strings.Contains(output, "No such device") {
            log.Printf("[DEBUG] Class %s not found on %s, returning zero stats.", markID, iface)
            return domain.NetDevStats{}, nil
        }
        return domain.NetDevStats{}, fmt.Errorf("failed to read tc class stats for %s on %s: %w, output: %s", markID, iface, err, output)
    }
    
    re := regexp.MustCompile(`Sent\s+(\d+)\s+bytes\s+(\d+)\s+(?:pkt|packets)s?`) 

    matches := re.FindStringSubmatch(output)
    
    if len(matches) < 3 {
        return domain.NetDevStats{}, nil 
    }

    bytes, _ := strconv.ParseUint(matches[1], 10, 64)

    return domain.NetDevStats{
        TxBytes: bytes, 
        RxBytes: 0,     
    }, nil
} 

func (l *LinuxDriver) CalculateIPRateMbps(ip string, iface string, classID uint16, currentStats domain.NetDevStats) (rateMbps float64, err error) {

	ipStateMutex.Lock()
	defer ipStateMutex.Unlock()

	// Utiliser l'IP + Interface comme clé pour distinguer l'upload et le download
	stateKey := fmt.Sprintf("%s-%s", ip, iface)

	state, exists := ipTrafficState[stateKey]
	if !exists {
		ipTrafficState[stateKey] = &domain.TrafficState{
			LastStats: currentStats,
			LastTime:  time.Now(),
			Mu:        sync.Mutex{},
		}
		return 0, nil
	}

	currentTime := time.Now()
	timeDiff := currentTime.Sub(state.LastTime).Seconds()

	if timeDiff == 0 {
		return 0, nil 
	}

	// Le compteur est stocké dans TxBytes
	currentBytes := currentStats.TxBytes
	lastBytes := state.LastStats.TxBytes

	// Calculer la différence d'octets
	byteDiff := int64(currentBytes) - int64(lastBytes)

	if byteDiff < 0 {
		byteDiff = 0
	} // Handle counter wrap-around

	// Calculer le taux en Octets/seconde (Bytes/sec)
	rateBps := float64(byteDiff) / timeDiff

	// Convertir Octets/sec en Mégabits/sec
	const factor = 8.0 / 1024.0 / 1024.0

	rateMbps = rateBps * factor

	// Mettre à jour l'état pour la prochaine lecture
	state.LastStats = currentStats
	state.LastTime = currentTime

	return rateMbps, nil
}