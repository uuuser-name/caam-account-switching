package monitor

import (
	"fmt"
	"time"

	"github.com/Dicklesworthstone/coding_agent_account_manager/internal/usage"
)

// Alert represents a monitor alert for a profile.
type Alert struct {
	Type    AlertType
	Message string
	Since   time.Time
}

// AlertType indicates the severity of a monitor alert.
type AlertType int

const (
	AlertNone AlertType = iota
	AlertWarning   // 70-85%
	AlertCritical  // 85-95%
	AlertExhausted // 95-100%
)

const (
	warningThreshold  = 70.0
	criticalThreshold = 85.0
	exhaustedThreshold = 95.0
)

func evaluateAlert(info *usage.UsageInfo, now time.Time) *Alert {
	if info == nil {
		return nil
	}

	percent := usagePercent(info)
	if percent <= 0 {
		return nil
	}

	switch {
	case percent >= exhaustedThreshold:
		return &Alert{
			Type:    AlertExhausted,
			Message: fmt.Sprintf("Usage at %.0f%% (limit nearly exhausted)", percent),
			Since:   now,
		}
	case percent >= criticalThreshold:
		return &Alert{
			Type:    AlertCritical,
			Message: fmt.Sprintf("Usage at %.0f%%", percent),
			Since:   now,
		}
	case percent >= warningThreshold:
		return &Alert{
			Type:    AlertWarning,
			Message: fmt.Sprintf("Usage at %.0f%%", percent),
			Since:   now,
		}
	default:
		return nil
	}
}

func usagePercent(info *usage.UsageInfo) float64 {
	if info == nil {
		return 0
	}
	window := info.MostConstrainedWindow()
	if window == nil {
		return 0
	}
	util := window.Utilization
	if util == 0 && window.UsedPercent > 0 {
		util = float64(window.UsedPercent) / 100.0
	}
	return util * 100
}
