package calendar

import (
	"fmt"
	"strings"

	"github.com/hr-agent/services/internal/config"
)

// NewFromConfig builds a calendar Provider from env-backed config.
func NewFromConfig(cfg *config.Config) (Provider, error) {
	switch strings.ToLower(strings.TrimSpace(cfg.CalendarProvider)) {
	case "", "memory":
		return NewMemoryProvider(), nil
	case "feishu", "lark":
		p, err := NewFeishuProvider(FeishuConfig{
			AppID:      cfg.FeishuAppID,
			AppSecret:  cfg.FeishuAppSecret,
			CalendarID: cfg.FeishuCalendarID,
			UserIDType: cfg.FeishuUserIDType,
			Timezone:   cfg.FeishuTimezone,
			Location:   cfg.FeishuLocation,
		})
		if err != nil {
			return nil, err
		}
		return p, nil
	default:
		return nil, fmt.Errorf("unknown CALENDAR_PROVIDER=%q (use memory|feishu)", cfg.CalendarProvider)
	}
}
