package system

import (
    "context"
    "fmt"
    "log"
    "os/exec"
    "bandwidth_controller_backend/internal/core/domain"
    "bandwidth_controller_backend/internal/core/port"
)

type LinuxDriver struct{}

func NewLinuxDriver() port.NetworkDriver {
    return &LinuxDriver{}
}


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