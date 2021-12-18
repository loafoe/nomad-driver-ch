/*
Copyright 2022 Andy Lo-A-Foe

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

	http://www.apache.org/licenses/LICENSE-2.0


Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package ch

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	docker "github.com/docker/docker/client"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/hashicorp/nomad/client/stats"
	"github.com/hashicorp/nomad/drivers/docker/docklog"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/structs"
)

// taskHandle should store all relevant runtime information
// such as process ID if this is a local task or other meta
// data if this driver deals with external APIs
type taskHandle struct {
	// stateLock syncs access to all fields below
	stateLock sync.RWMutex

	totalCpuStats  *stats.CpuStats
	userCpuStats   *stats.CpuStats
	systemCpuStats *stats.CpuStats

	logger      hclog.Logger
	taskConfig  *drivers.TaskConfig
	procState   drivers.TaskState
	startedAt   time.Time
	completedAt time.Time
	exitResult  *drivers.ExitResult
	net         *drivers.DriverNetwork

	// TODO: add any extra relevant information about the task.
	dockerClient *docker.Client
	containerID  string
	chContainer  CHContainer

	// dlogger
	dlogger             docklog.DockerLogger
	dloggerPluginClient *plugin.Client

	// mirror
	listeners []chan bool
}

func (h *taskHandle) buildState() *TaskState {
	s := &TaskState{
		ContainerID: h.containerID,
		//DriverNetwork: h.net,
	}
	if h.dloggerPluginClient != nil {
		s.ReattachConfig = structs.ReattachConfigFromGoPlugin(h.dloggerPluginClient.ReattachConfig())
	}
	return s
}

func (h *taskHandle) TaskStatus() *drivers.TaskStatus {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()

	return &drivers.TaskStatus{
		ID:          h.taskConfig.ID,
		Name:        h.taskConfig.Name,
		State:       h.procState,
		StartedAt:   h.startedAt,
		CompletedAt: h.completedAt,
		ExitResult:  h.exitResult,
		DriverAttributes: map[string]string{
			"container_id": h.containerID,
		},
		NetworkOverride: h.net,
	}
}

func (h *taskHandle) IsRunning() bool {
	h.stateLock.RLock()
	defer h.stateLock.RUnlock()
	return h.procState == drivers.TaskStateRunning
}

func (h *taskHandle) run() {
	defer h.shutdownLogger()
	defer h.shutdownMirrorListeners()

	h.stateLock.Lock()
	if h.exitResult == nil {
		h.exitResult = &drivers.ExitResult{}
	}
	h.stateLock.Unlock()
	h.logger.Info("waiting for container to exit", "container_id", h.containerID)
	waitC, errC := h.dockerClient.ContainerWait(context.Background(), h.containerID, container.WaitConditionNotRunning)
	h.stateLock.Lock()
	defer h.stateLock.Unlock()

	select {
	case exitStatus := <-waitC:
		h.logger.Info("container exit message received", "exit_status", hclog.Fmt("%+v", exitStatus))
		h.procState = drivers.TaskStateExited
		h.exitResult.ExitCode = int(exitStatus.StatusCode)
	case err := <-errC:
		h.logger.Info("container wait error", "error", err.Error())
		h.exitResult.Err = err
		h.procState = drivers.TaskStateExited
	}
	h.completedAt = time.Now()
}

func (h *taskHandle) stats(ctx context.Context, interval time.Duration) (<-chan *drivers.TaskResourceUsage, error) {
	ch := make(chan *drivers.TaskResourceUsage)
	go h.handleStats(ctx, ch, interval)
	return ch, nil
}

func (h *taskHandle) handleStats(ctx context.Context, ch chan *drivers.TaskResourceUsage, interval time.Duration) {
	defer close(ch)
	timer := time.NewTimer(0)
	for {
		select {
		case <-ctx.Done():
			return

		case <-timer.C:
			timer.Reset(interval)
		}
		var decodedStats types.StatsJSON
		containerStats, err := h.dockerClient.ContainerStats(ctx, h.containerID, false)
		if err != nil {
			h.logger.Error("failed to get container cpu stats", "error", err)
			continue
		}
		decoder := json.NewDecoder(containerStats.Body)
		err = decoder.Decode(&decodedStats)
		if err != nil {
			h.logger.Error("failed to decode container stats", "error", err)
			_ = containerStats.Body.Close()
			continue
		}
		_ = containerStats.Body.Close()
		taskResUsage := h.DockerStatsToTaskResourceUsage(&decodedStats)
		select {
		case <-ctx.Done():
			return
		case ch <- taskResUsage:
			h.logger.Debug("sent usage", "cpu_percent", hclog.Fmt("%+v", taskResUsage.ResourceUsage.CpuStats.Percent))
		}
	}
}

func (h *taskHandle) shutdownMirrorListeners() {
	if h.listeners == nil {
		return
	}
	for _, l := range h.listeners {
		l <- true
	}
}

func (h *taskHandle) shutdownLogger() {
	if h.dlogger == nil {
		return
	}

	if err := h.dlogger.Stop(); err != nil {
		h.logger.Error("failed to stop docker logger process during StopTask",
			"error", err, "logger_pid", h.dloggerPluginClient.ReattachConfig().Pid)
	}
	h.dloggerPluginClient.Kill()
}
