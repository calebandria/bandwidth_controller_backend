package service

import (
	"bandwidth_controller_backend/internal/core/domain"
	"bandwidth_controller_backend/internal/core/port"
	"context"
	"errors"
)

const (
	ModeGlobalTBF = "TBF"
	ModeGlobalHTB = "HTB"
)

type QoSManager struct {
	driver      port.NetworkDriver
	statsStream chan domain.TrafficStat
	threshold   uint64
	ilan        string
	inet        string
	activeMode  string
}

func NewQoSManager(driver port.NetworkDriver, ilan string, inet string) *QoSManager {
	mgr := &QoSManager{
		driver:      driver,
		statsStream: make(chan domain.TrafficStat, 100),
		threshold:   100000000,
		ilan:        ilan,
		inet:        inet,
		activeMode:  ModeGlobalTBF,
	}
	return mgr
}

func (s *QoSManager) SetupGlobalQoS(ctx context.Context, ilan string, inet string, maxRate string) error {
	if err := s.driver.SetupHTBStructure(ctx, ilan, inet, maxRate); err != nil {
		return err
	}
	s.activeMode = ModeGlobalHTB
	return nil
}

func (s *QoSManager) UpdateGlobalLimit(ctx context.Context, rule domain.QoSRule) error {
	if s.activeMode != ModeGlobalHTB {
		return errors.New("HTB mode is not active. Please call /qos/setup first")
	}
	return s.driver.ApplyGlobalShaping(ctx, rule)
}

func (s *QoSManager) SetSimpleGlobalLimit(ctx context.Context, rule domain.QoSRule) error {
	if s.activeMode == ModeGlobalHTB {
		return errors.New("cannot apply TBF limit while HTB is active. Reset first")
	}
	s.activeMode = ModeGlobalTBF
	return s.driver.ApplyShaping(ctx, rule)
}

func (s *QoSManager) ResetQoS(ctx context.Context, ilan string, inet string,) error {
	return s.driver.ResetShaping(ctx, ilan, inet);
}

func (s *QoSManager) GetConnectedLANIPs(ctx context.Context, ilan string)([]string, error){
	return s.driver.GetConnectedLANIPs(ctx, ilan);
}

