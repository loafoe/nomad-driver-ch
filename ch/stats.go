package ch

import (
	"runtime"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/hashicorp/nomad/helper/stats"
	"github.com/hashicorp/nomad/plugins/drivers"
)

var (
	DockerMeasuredCPUStats = []string{"Throttled Periods", "Throttled Time", "Percent"}

	// cgroup-v2 only exposes a subset of memory stats
	DockerCgroupV1MeasuredMemStats = []string{"RSS", "Cache", "Swap", "Usage", "Max Usage"}
)

func (h *taskHandle) DockerStatsToTaskResourceUsage(s *types.StatsJSON) *drivers.TaskResourceUsage {
	measuredMems := DockerCgroupV1MeasuredMemStats

	ms := &drivers.MemoryStats{
		RSS:      s.MemoryStats.Stats["rss"],
		Cache:    s.MemoryStats.Stats["cache"],
		Swap:     s.MemoryStats.Stats["swap"],
		Usage:    s.MemoryStats.Usage,
		MaxUsage: s.MemoryStats.MaxUsage,
		Measured: measuredMems,
	}

	cs := &drivers.CpuStats{
		ThrottledPeriods: s.CPUStats.ThrottlingData.ThrottledPeriods,
		ThrottledTime:    s.CPUStats.ThrottlingData.ThrottledTime,
		Measured:         DockerMeasuredCPUStats,
	}

	// Calculate percentage
	cs.Percent = h.totalCpuStats.Percent(float64(s.CPUStats.CPUUsage.TotalUsage))
	cs.SystemMode = CalculateCPUPercent(
		s.CPUStats.CPUUsage.UsageInKernelmode, s.PreCPUStats.CPUUsage.UsageInKernelmode,
		s.CPUStats.CPUUsage.TotalUsage, s.PreCPUStats.CPUUsage.TotalUsage, runtime.NumCPU())
	cs.UserMode = CalculateCPUPercent(
		s.CPUStats.CPUUsage.UsageInUsermode, s.PreCPUStats.CPUUsage.UsageInUsermode,
		s.CPUStats.CPUUsage.TotalUsage, s.PreCPUStats.CPUUsage.TotalUsage, runtime.NumCPU())
	cs.TotalTicks = (cs.Percent / 100) * stats.TotalTicksAvailable() / float64(runtime.NumCPU())

	return &drivers.TaskResourceUsage{
		ResourceUsage: &drivers.ResourceUsage{
			MemoryStats: ms,
			CpuStats:    cs,
		},
		Timestamp: time.Now().UTC().UnixNano(),
	}
}

func CalculateCPUPercent(newSample, oldSample, newTotal, oldTotal uint64, cores int) float64 {
	numerator := newSample - oldSample
	denom := newTotal - oldTotal
	if numerator <= 0 || denom <= 0 {
		return 0.0
	}

	return (float64(numerator) / float64(denom)) * float64(cores) * 100.0
}
