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
type ActiveIPConfig map[string]uint16

type LinuxDriver struct{
	state domain.TrafficState
	activeIPs ActiveIPConfig 
	mu sync.Mutex
	nextClassID uint16

}

func NewLinuxDriver() port.NetworkDriver {
    return &LinuxDriver{}
}

var trafficState = make(map[string]*domain.TrafficState)
var stateMutex sync.RWMutex

func applyTcCommand(ctx context.Context, args []string, iface string) error {
    cmd := exec.CommandContext(ctx, "tc", args...)
    output, err := cmd.CombinedOutput()
    
    if err != nil {
        errMsg := fmt.Sprintf("Command failed on %s! Command: tc %v\nOutput: %s", iface, args, string(output))
        log.Println("ERROR:", errMsg)
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
		log.Println("ERROR:", errMsg)
		return fmt.Errorf("iptables command failed: %s", string(output))
	}
	return nil
}

func ExecCommand(ctx context.Context, name string, arg ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, arg...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), fmt.Errorf("error executing command: %s, output: %s", err, string(output))
	}
	return string(output), nil
}


func (l *LinuxDriver) ApplyShaping(ctx context.Context, rule domain.QoSRule) error {
    _ = l.ResetShaping(ctx, rule.LanInterface, rule.WanInterface)

    lan_args := []string{
        "qdisc", "add", "dev", rule.LanInterface,
        "root", "tbf",
        "rate", rule.Bandwidth,
        "burst", "10k", 
        "latency", rule.Latency,
    }

    if err := applyTcCommand(ctx, lan_args, rule.LanInterface); err != nil {
        return fmt.Errorf("lan shaping failed: %w", err)
    }
    log.Printf("QoS rule successfully applied (TBF mode) on %s: Rate=%s", rule.LanInterface, rule.Bandwidth)

    wan_args := []string{
        "qdisc", "add", "dev", rule.WanInterface,
        "root", "tbf",
        "rate", rule.Bandwidth,
        "burst", "10k",
        "latency", rule.Latency,
    }

    if err := applyTcCommand(ctx, wan_args, rule.WanInterface); err != nil {
        return fmt.Errorf("wan shaping failed: %w", err)
    }
    log.Printf("QoS rule successfully applied (TBF mode) on %s: Rate=%s", rule.WanInterface, rule.Bandwidth)

    return nil
}


func (l *LinuxDriver) SetupHTBStructure(ctx context.Context, ilan string, iwan string, totalBandwidth string) error {
    _ = l.ResetShaping(ctx, ilan, iwan)

    argsQdisc := []string{"qdisc", "add", "dev", ilan, "root", "handle", "1:", "htb", "default", "1"}
    if err := applyTcCommand(ctx, argsQdisc, ilan); err != nil {
        return fmt.Errorf("htb qdisc setup failed on %s: %w", ilan, err)
    }

    argsRootClass := []string{"class", "add", "dev", ilan, "parent", "1:", "classid", "1:1", "htb",
        "rate", totalBandwidth, "ceil", totalBandwidth}
    if err := applyTcCommand(ctx, argsRootClass, ilan); err != nil {
        return fmt.Errorf("htb root class setup failed on %s: %w", ilan, err)
    }

    log.Printf("HTB structure set up on %s with global capacity: %s (Root Class 1:1)", ilan, totalBandwidth)


    argsQdisc = []string{"qdisc", "add", "dev", iwan, "root", "handle", "1:", "htb", "default", "1"}
    if err := applyTcCommand(ctx, argsQdisc, iwan); err != nil {
        return fmt.Errorf("htb qdisc setup failed on %s: %w", iwan, err)
    }

    argsRootClass = []string{"class", "add", "dev", iwan, "parent", "1:", "classid", "1:1", "htb",
        "rate", totalBandwidth, "ceil", totalBandwidth}
    if err := applyTcCommand(ctx, argsRootClass, iwan); err != nil {
        return fmt.Errorf("htb root class setup failed on %s: %w", iwan, err)
    }

    log.Printf("HTB structure set up on %s with global capacity: %s (Root Class 1:1)", iwan, totalBandwidth)

    return nil
}


func (l *LinuxDriver) ApplyGlobalShaping(ctx context.Context, rule domain.QoSRule) error {
    args := []string{"class", "change", "dev", rule.LanInterface, "parent", "1:",
        "classid", "1:1", "htb", "rate", rule.Bandwidth, "ceil", rule.Bandwidth}

    if err := applyTcCommand(ctx, args, rule.LanInterface); err != nil {
        return fmt.Errorf("htb global rate change failed on %s: %w", rule.LanInterface, err)
    }
    log.Printf("Global HTB rate successfully set on %s: Rate=%s", rule.LanInterface, rule.Bandwidth)

    args = []string{"class", "change", "dev", rule.WanInterface, "parent", "1:",
        "classid", "1:1", "htb", "rate", rule.Bandwidth, "ceil", rule.Bandwidth}

    if err := applyTcCommand(ctx, args, rule.WanInterface); err != nil {
        return fmt.Errorf("htb global rate change failed on %s: %w", rule.WanInterface, err)
    }

    log.Printf("Global HTB rate successfully set on %s: Rate=%s", rule.WanInterface, rule.Bandwidth)
    
    return nil
}

func (l *LinuxDriver) ResetShaping(ctx context.Context, ilan string, iwan string) error {
    var firstErr error
    
    delQdisc := func(iface string) error {
        cmd := exec.CommandContext(ctx, "tc", "qdisc", "del", "dev", iface, "root")
        output, err := cmd.CombinedOutput()
        
        if err != nil {
            outputStr := string(output)
            if outputStr == "RTNETLINK answers: No such file or directory\n" || outputStr == "RTNETLINK answers: Invalid argument\n" {
                return nil
            }
            
            log.Printf("ERROR: Failed to reset shaping on %s. Output: %s", iface, outputStr)
            return fmt.Errorf("failed to delete qdisc on %s: %s", iface, outputStr)
        }
        log.Printf("QDisc reset successful on %s.", iface)
        return nil
    }

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

func (l *LinuxDriver) GetConnectedLANIPs(ctx context.Context, ilan string) ([]string, error) {
	output, err := ExecCommand(ctx, "ip", "neighbor", "show", "dev", ilan)
	if err != nil {
		log.Printf("ip neighbor command failed for %s: %v, output: %s", ilan, err, output)
		return nil, fmt.Errorf("impossible de lire la table ARP pour %s: %w", ilan, err)
	}

	var ips []string
	ipRegex := regexp.MustCompile(`(\b\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}\b)|(\b[0-9a-fA-F:]+\b)`)

	lines := strings.SplitSeq(output, "\n")
	
	for line := range lines {
		trimmedLine := strings.TrimSpace(line)
		
		if strings.Contains(trimmedLine, "REACHABLE") || strings.Contains(trimmedLine, "STALE") || strings.Contains(trimmedLine, "DELAY") {
			matches := ipRegex.FindAllString(trimmedLine, -1)
			
			for _, match := range matches {
				if !strings.HasPrefix(match, "fe80:") {
					ips = append(ips, match)
					break 
				}
			}
		}
	}

	return ips, nil
}

func (l *LinuxDriver)GetInstantaneousNetDevStats(iface string) (domain.NetDevStats, error) {
	data, err := os.ReadFile("/proc/net/dev")
	if err != nil {
		return domain.NetDevStats{}, fmt.Errorf("impossible de lire /proc/net/dev: %w", err)
	}

	lines := strings.SplitSeq(string(data), "\n")

	for line := range lines {
		trimmedLine := strings.TrimSpace(line)
		
		if strings.HasPrefix(trimmedLine, iface+":") {
			fieldsStr := strings.TrimPrefix(trimmedLine, iface+":")
			fields := strings.Fields(fieldsStr)
			
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

func (l *LinuxDriver)CalculateRateMbps(iface string, currentStats domain.NetDevStats) (txRateMbps float64, rxRateMbps float64, err error) {
 	stateMutex.Lock()
	state, exists := trafficState[iface]
	if !exists {
		// Initialisation de l'état
		trafficState[iface] = &domain.TrafficState{
			LastStats: currentStats,
			LastTime: time.Now(),
		}
		stateMutex.Unlock()
		// Pas de débit à calculer lors de la première lecture
		return 0, 0, nil
	}
	stateMutex.Unlock()

	state.Mu.Lock()
	defer state.Mu.Unlock()

	// Calcul de l'intervalle de temps
	currentTime := time.Now()
	timeDiff := currentTime.Sub(state.LastTime).Seconds()

	if timeDiff == 0 {
		return 0, 0, nil // Évite la division par zéro
	}

	// Calculer la différence d'octets
	txDiff := int64(currentStats.TxBytes) - int64(state.LastStats.TxBytes)
	rxDiff := int64(currentStats.RxBytes) - int64(state.LastStats.RxBytes)

	if txDiff < 0 { txDiff = 0 }
	if rxDiff < 0 { rxDiff = 0 }

	// Calculer le taux en Octets/seconde (Bytes/sec)
	txRateBps := float64(txDiff) / timeDiff
	rxRateBps := float64(rxDiff) / timeDiff
    
    // Convertir Octets/sec en Mégabits/sec (Bps * 8 / 1024 / 1024)
    const factor = 8.0 / 1024.0 / 1024.0

    txRateMbps = txRateBps * factor
    rxRateMbps = rxRateBps * factor
    
	// Mettre à jour l'état pour la prochaine lecture
	state.LastStats = currentStats
	state.LastTime = currentTime

	return txRateMbps, rxRateMbps, nil
}

func (l *LinuxDriver) AddIPRateLimit(ctx context.Context, ip string, rule domain.QoSRule) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	classID := l.nextClassID
	l.nextClassID++
	
	l.activeIPs[ip] = classID
	log.Printf("Assigning HTB Class/Mark ID 1:%d to IP %s", classID, ip)

	// Convert classID to hexadecimal string for tc
	classIDStr := fmt.Sprintf("%x", classID) 

	// 2. Setup HTB Class on LAN (for Tx shaping)
	// Example: tc class add dev eth0 parent 1:1 classid 1:10 htb rate 1mbit ceil 1mbit
	lanArgs := []string{"class", "add", "dev", rule.LanInterface, "parent", "1:1", 
		"classid", fmt.Sprintf("1:%s", classIDStr), "htb", 
		"rate", rule.Bandwidth, "ceil", rule.Bandwidth} 
	if err := applyTcCommand(ctx, lanArgs, rule.LanInterface); err != nil {
		delete(l.activeIPs, ip) // Cleanup state on failure
		return fmt.Errorf("failed to add HTB class 1:%s on %s: %w", classIDStr, rule.LanInterface, err)
	}
	
	// 3. Setup IPTABLES MARK on LAN (using the same ID as the class)
	// Example: iptables -t mangle -A POSTROUTING -o eth0 -s 192.168.1.100 -j MARK --set-mark 10
	markID := fmt.Sprintf("%d", classID)
	iptablesArgs := []string{"-t", "mangle", "-A", "POSTROUTING", "-o", rule.LanInterface, 
		"-s", ip, "-j", "MARK", "--set-mark", markID}
	if err := applyIptablesCommand(ctx, iptablesArgs); err != nil {
		l.RemoveIPRateLimit(ctx, ip, rule) // Full cleanup
		return fmt.Errorf("failed to add iptables mark for %s: %w", ip, err)
	}

	// 4. Setup TC Filter on LAN
	// Example: tc filter add dev eth0 parent 1: protocol ip prio 1 handle 10 fw classid 1:10
	filterArgs := []string{"filter", "add", "dev", rule.LanInterface, "parent", "1:", "protocol", "ip", 
		"prio", "1", "handle", classIDStr, "fw", "classid", fmt.Sprintf("1:%s", classIDStr)}
	if err := applyTcCommand(ctx, filterArgs, rule.LanInterface); err != nil {
		l.RemoveIPRateLimit(ctx, ip, rule) // Full cleanup
		return fmt.Errorf("failed to add tc filter for %s: %w", ip, err)
	}
	
	// 2. Setup HTB Class on WAN
	// Exemple: tc class add dev rule.WanInterface parent 1:1 classid 1:10 htb rate 1mbit ceil 1mbit
	wanArgs := []string{"class", "add", "dev", rule.WanInterface, "parent", "1:1", 
		"classid", fmt.Sprintf("1:%s", classIDStr), "htb", 
		"rate", rule.Bandwidth, "ceil", rule.Bandwidth} 
	if err := applyTcCommand(ctx, wanArgs, rule.WanInterface); err != nil {
		delete(l.activeIPs, ip) // Nettoyage de l'état
		return fmt.Errorf("failed to add HTB class 1:%s on %s: %w", classIDStr, rule.WanInterface, err)
	}
	
	// 3. Setup IPTABLES MARK on WAN (POSTROUTING pour trafic sortant)
	// Le trafic de l'IP Source du LAN (-s) qui sort de l'interface WAN (-o) est marqué.
	// Exemple: iptables -t mangle -A POSTROUTING -o rule.WanInterface -s 192.168.1.100 -j MARK --set-mark 10
	iptablesTxArgs := []string{"-t", "mangle", "-A", "POSTROUTING", "-o", rule.WanInterface, 
		"-s", ip, "-j", "MARK", "--set-mark", markID}
	if err := applyIptablesCommand(ctx, iptablesTxArgs); err != nil {
		l.RemoveIPRateLimit(ctx, ip, rule) // Nettoyage complet
		return fmt.Errorf("failed to add iptables Tx mark for %s: %w", ip, err)
	}

	// 4. Setup TC Filter on WAN
	// Exemple: tc filter add dev rule.WanInterface parent 1: protocol ip prio 1 handle 10 fw classid 1:10
	filterTxArgs := []string{"filter", "add", "dev", rule.WanInterface, "parent", "1:", "protocol", "ip", 
		"prio", "1", "handle", classIDStr, "fw", "classid", fmt.Sprintf("1:%s", classIDStr)}
	if err := applyTcCommand(ctx, filterTxArgs, rule.WanInterface); err != nil {
		l.RemoveIPRateLimit(ctx, ip, rule) // Nettoyage complet
		return fmt.Errorf("failed to add tc Tx filter for %s: %w", ip, err)
	}

	log.Printf("IP QoS successfully applied to %s on %s: Rate=%s, ClassID=1:%s", ip, rule.LanInterface, rule.Bandwidth, classIDStr)

	return nil
}

func (l *LinuxDriver) RemoveIPRateLimit(ctx context.Context, ip string, rule domain.QoSRule) error {
	l.mu.Lock()
	defer l.mu.Unlock()

	classID, ok := l.activeIPs[ip]
	if !ok {
		return fmt.Errorf("no HTB class found for IP %s", ip)
	}

	classIDStr := fmt.Sprintf("%x", classID)
	markID := fmt.Sprintf("%d", classID)
	
	// --- PART 1: WAN (Transmission / Upload) Cleanup ---
	
	// 1. Delete TC Filter on WAN
	filterTxArgs := []string{"filter", "del", "dev", rule.WanInterface, "parent", "1:", "protocol", "ip", "prio", "1", "handle", classIDStr, "fw"}
	if err := applyTcCommand(ctx, filterTxArgs, rule.WanInterface); err != nil {
		log.Printf("Warning: Failed to delete tc Tx filter for %s on %s: %v", ip, rule.WanInterface, err)
	}
	
	// 2. Delete HTB Class on WAN
	wanArgs := []string{"class", "del", "dev", rule.WanInterface, "parent", "1:1", "classid", fmt.Sprintf("1:%s", classIDStr)} 
	if err := applyTcCommand(ctx, wanArgs, rule.WanInterface); err != nil {
		log.Printf("Warning: Failed to delete HTB class 1:%s on %s: %v", classIDStr, rule.WanInterface, err)
	}

	// 3. Delete IPTABLES MARK on WAN (POSTROUTING, -D to delete)
	iptablesTxArgs := []string{"-t", "mangle", "-D", "POSTROUTING", "-o", rule.WanInterface, 
		"-s", ip, "-j", "MARK", "--set-mark", markID}
	if err := applyIptablesCommand(ctx, iptablesTxArgs); err != nil {
		log.Printf("Warning: Failed to delete iptables Tx mark for %s: %v", ip, err)
	}
	
	// --- PART 2: LAN (Transmission / Download) Cleanup ---
	
	// 4. Delete TC Filter on LAN
	filterRxArgs := []string{"filter", "del", "dev", rule.WanInterface, "parent", "1:", "protocol", "ip", "prio", "1", "handle", classIDStr, "fw"}
	if err := applyTcCommand(ctx, filterRxArgs, rule.WanInterface); err != nil {
		log.Printf("Warning: Failed to delete tc Rx filter for %s on %s: %v", ip, rule.WanInterface, err)
	}
	
	// 5. Delete HTB Class on LAN
	lanArgs := []string{"class", "del", "dev", rule.WanInterface, "parent", "1:1", "classid", fmt.Sprintf("1:%s", classIDStr)} 
	if err := applyTcCommand(ctx, lanArgs, rule.WanInterface); err != nil {
		log.Printf("Warning: Failed to delete HTB class 1:%s on %s: %v", classIDStr, rule.WanInterface, err)
	}

	// 6. Delete IPTABLES MARK on LAN (POSTROUTING, -D to delete)
	iptablesRxArgs := []string{"-t", "mangle", "-D", "POSTROUTING", "-o", rule.WanInterface, 
		"-d", ip, "-j", "MARK", "--set-mark", markID}
	if err := applyIptablesCommand(ctx, iptablesRxArgs); err != nil {
		log.Printf("Warning: Failed to delete iptables Rx mark for %s: %v", ip, err)
	}
	
	delete(l.activeIPs, ip)
	log.Printf("IP QoS successfully removed for %s", ip)
	return nil
}


