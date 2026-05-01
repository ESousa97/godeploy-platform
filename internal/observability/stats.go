package observability

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"strings"
	"time"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

type ContainerStat struct {
	ID     string            `json:"id"`
	Name   string            `json:"name"`
	Image  string            `json:"image"`
	Labels map[string]string `json:"labels,omitempty"`
	State  string            `json:"state,omitempty"`
	Status string            `json:"status,omitempty"`

	CPUPercent float64 `json:"cpu_percent"`

	MemUsageBytes int64   `json:"mem_usage_bytes"`
	MemLimitBytes int64   `json:"mem_limit_bytes"`
	MemPercent    float64 `json:"mem_percent"`

	CollectedAt time.Time `json:"collected_at"`
}

type StatsResponse struct {
	Containers  []ContainerStat `json:"containers"`
	CollectedAt time.Time       `json:"collected_at"`
}

type Collector struct {
	docker *client.Client
}

func NewCollector(docker *client.Client) *Collector {
	return &Collector{docker: docker}
}

func (c *Collector) Collect(ctx context.Context) (StatsResponse, error) {
	now := time.Now().UTC()

	list, err := c.docker.ContainerList(ctx, container.ListOptions{All: false})
	if err != nil {
		return StatsResponse{}, fmt.Errorf("listar containers: %w", err)
	}

	out := make([]ContainerStat, 0, len(list))
	for _, ctr := range list {
		name := ""
		if len(ctr.Names) > 0 {
			name = strings.TrimPrefix(strings.TrimSpace(ctr.Names[0]), "/")
		}

		st, err := c.collectOne(ctx, ctr.ID)
		if err != nil {
			// Em caso de erro pontual, ainda retornamos o restante.
			// O caller pode decidir como exibir/monitorar estes casos.
			continue
		}
		st.ID = ctr.ID
		st.Name = name
		st.Image = strings.TrimSpace(ctr.Image)
		st.Labels = ctr.Labels
		st.State = strings.TrimSpace(ctr.State)
		st.Status = strings.TrimSpace(ctr.Status)
		st.CollectedAt = now
		out = append(out, st)
	}

	return StatsResponse{Containers: out, CollectedAt: now}, nil
}

func (c *Collector) collectOne(ctx context.Context, containerID string) (ContainerStat, error) {
	resp, err := c.docker.ContainerStats(ctx, containerID, false)
	if err != nil {
		return ContainerStat{}, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return ContainerStat{}, err
	}

	var v struct {
		CPUStats struct {
			CPUUsage struct {
				TotalUsage        uint64   `json:"total_usage"`
				PercpuUsage       []uint64 `json:"percpu_usage"`
				UsageInKernelmode uint64   `json:"usage_in_kernelmode"`
				UsageInUsermode   uint64   `json:"usage_in_usermode"`
			} `json:"cpu_usage"`
			SystemCPUUsage uint64 `json:"system_cpu_usage"`
			OnlineCPUs     uint32 `json:"online_cpus"`
		} `json:"cpu_stats"`
		PreCPUStats struct {
			CPUUsage struct {
				TotalUsage uint64 `json:"total_usage"`
			} `json:"cpu_usage"`
			SystemCPUUsage uint64 `json:"system_cpu_usage"`
			OnlineCPUs     uint32 `json:"online_cpus"`
		} `json:"precpu_stats"`
		MemoryStats struct {
			Usage uint64 `json:"usage"`
			Limit uint64 `json:"limit"`
		} `json:"memory_stats"`
	}

	if err := json.Unmarshal(body, &v); err != nil {
		return ContainerStat{}, err
	}

	cpuPercent := computeCPUPercent(v.CPUStats.SystemCPUUsage, v.PreCPUStats.SystemCPUUsage, v.CPUStats.CPUUsage.TotalUsage, v.PreCPUStats.CPUUsage.TotalUsage, v.CPUStats.OnlineCPUs, v.PreCPUStats.OnlineCPUs, v.CPUStats.CPUUsage.PercpuUsage)

	usage := int64(v.MemoryStats.Usage)
	limit := int64(v.MemoryStats.Limit)
	memPercent := 0.0
	if limit > 0 {
		memPercent = (float64(usage) / float64(limit)) * 100.0
	}

	return ContainerStat{
		CPUPercent:    cpuPercent,
		MemUsageBytes: usage,
		MemLimitBytes: limit,
		MemPercent:    clampPercent(memPercent),
	}, nil
}

func computeCPUPercent(systemNow, systemPrev, cpuNow, cpuPrev uint64, onlineNow, onlinePrev uint32, perCPU []uint64) float64 {
	cpuDelta := float64(cpuNow - cpuPrev)
	systemDelta := float64(systemNow - systemPrev)
	if cpuDelta <= 0 || systemDelta <= 0 {
		return 0
	}

	online := float64(onlineNow)
	if online <= 0 {
		// fallback: usa len(percpu_usage) se online_cpus não vier preenchido
		if n := len(perCPU); n > 0 {
			online = float64(n)
		} else if onlinePrev > 0 {
			online = float64(onlinePrev)
		} else {
			online = 1
		}
	}

	return clampPercent((cpuDelta / systemDelta) * online * 100.0)
}

func clampPercent(v float64) float64 {
	if math.IsNaN(v) || math.IsInf(v, 0) {
		return 0
	}
	if v < 0 {
		return 0
	}
	if v > 100*1024 {
		// em máquinas grandes (muitos cores) pode passar de 100% facilmente;
		// ainda assim colocamos um teto alto para evitar valores absurdos.
		return 100 * 1024
	}
	return v
}
