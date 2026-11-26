package repo

import (
	"testing"

	"go-backend/internal/store/model"
)

func TestTrafficDeltaAndRebootDetection(t *testing.T) {
	base := model.NodeTrafficBaseline{
		NodeID:    1,
		InBytes:   500_000_000_000,
		OutBytes:  300_000_000_000,
		Uptime:    100_000,
		Timestamp: 1_700_000_000_000,
	}

	t.Run("normal increment", func(t *testing.T) {
		in, out := trafficDelta(base, &model.NodeMetric{
			NetInBytes:  500_000_001_000,
			NetOutBytes: 300_000_002_000,
			Uptime:      100_060,
			Timestamp:   1_700_000_060_000,
		})
		if in != 1000 || out != 2000 {
			t.Fatalf("got (%d, %d), want (1000, 2000)", in, out)
		}
	})

	t.Run("virtual interface removed without reboot", func(t *testing.T) {
		in, out := trafficDelta(base, &model.NodeMetric{
			NetInBytes:  490_000_000_000,
			NetOutBytes: 300_000_005_000,
			Uptime:      100_060,
			Timestamp:   1_700_000_060_000,
		})
		if in != 0 || out != 5000 {
			t.Fatalf("interface removal should not recount boot total: got (%d, %d), want (0, 5000)", in, out)
		}
	})

	t.Run("actual reboot uptime decreases", func(t *testing.T) {
		in, out := trafficDelta(base, &model.NodeMetric{
			NetInBytes:  1024,
			NetOutBytes: 2048,
			Uptime:      30,
			Timestamp:   1_700_000_120_000,
		})
		if in != 1024 || out != 2048 {
			t.Fatalf("reboot got (%d, %d), want (1024, 2048)", in, out)
		}
	})

	t.Run("reboot while offline longer than previous uptime", func(t *testing.T) {
		in, out := trafficDelta(base, &model.NodeMetric{
			NetInBytes:  4096,
			NetOutBytes: 8192,
			Uptime:      120_000,
			Timestamp:   1_700_200_000_000, // boot moved forward by 180_000s > rebootSlackSeconds
		})
		if in != 4096 || out != 8192 {
			t.Fatalf("offline reboot got (%d, %d), want (4096, 8192)", in, out)
		}
	})
}
