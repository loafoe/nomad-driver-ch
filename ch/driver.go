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
	"errors"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/docker/docker/api/types"
	docker "github.com/docker/docker/client"
	"github.com/hashicorp/go-hclog"
	"github.com/hashicorp/go-plugin"
	"github.com/hashicorp/nomad/client/stats"
	"github.com/hashicorp/nomad/drivers/docker/docklog"
	"github.com/hashicorp/nomad/drivers/shared/eventer"
	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/hclspec"
	"github.com/hashicorp/nomad/plugins/shared/structs"
	"github.com/loafoe/nomad-driver-ch/forwarder"
	"golang.org/x/sys/unix"
)

const (
	// pluginName is the name of the plugin
	// this is used for logging and (along with the version) for uniquely
	// identifying plugin binaries fingerprinted by the client
	pluginName = "ch"

	// pluginVersion allows the client to identify and use newer versions of
	// an installed plugin
	pluginVersion = "v0.1.0"

	// fingerprintPeriod is the interval at which the plugin will send
	// fingerprint responses
	fingerprintPeriod = 30 * time.Second

	// taskHandleVersion is the version of task handle which this plugin sets
	// and understands how to decode
	// this is used to allow modification and migration of the task schema
	// used by the plugin
	taskHandleVersion = 1
)

var (
	// pluginInfo describes the plugin
	pluginInfo = &base.PluginInfoResponse{
		Type:              base.PluginTypeDriver,
		PluginApiVersions: []string{drivers.ApiVersion010},
		PluginVersion:     pluginVersion,
		Name:              pluginName,
	}

	// configSpec is the specification of the plugin's configuration
	// this is used to validate the configuration specified for the plugin
	// on the client.
	// this is not global, but can be specified on a per-client basis.
	configSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		"enabled": hclspec.NewDefault(
			hclspec.NewAttr("enabled", "bool", false),
			hclspec.NewLiteral("true"),
		),
		"runtime": hclspec.NewDefault(
			hclspec.NewAttr("runtime", "string", false),
			hclspec.NewLiteral(`"runc"`),
		),
		// TODO: define plugin's agent configuration schema.
		//
		// The schema should be defined using HCL specs and it will be used to
		// validate the agent configuration provided by the user in the
		// `plugin` stanza (https://www.nomadproject.io/docs/configuration/plugin.html).
		//
		// For example, for the schema below a valid configuration would be:
		//
		//   plugin "hello-driver-plugin" {
		//     config {
		//       shell = "fish"
		//     }
		//   }
	})

	// taskConfigSpec is the specification of the plugin's configuration for
	// a task
	// this is used to validate the configuration specified for the plugin
	// when a job is submitted.
	taskConfigSpec = hclspec.NewObject(map[string]*hclspec.Spec{
		"image":   hclspec.NewAttr("image", "string", true),
		"command": hclspec.NewAttr("command", "string", false),
		"ports":   hclspec.NewAttr("ports", "list(string)", false),
		"auth": hclspec.NewBlock("auth", false, hclspec.NewObject(map[string]*hclspec.Spec{
			"username": hclspec.NewAttr("username", "string", true),
			"password": hclspec.NewAttr("password", "string", true),
		})),
		"copy": hclspec.NewAttr("copy", "list(string)", false),
		"args": hclspec.NewAttr("args", "list(string)", false),
	})

	// capabilities indicates what optional features this driver supports
	// this should be set according to the target run time.
	capabilities = &drivers.Capabilities{
		// TODO: set plugin's capabilities
		//
		// The plugin's capabilities signal Nomad which extra functionalities
		// are supported. For a list of available options check the docs page:
		// https://godoc.org/github.com/hashicorp/nomad/plugins/drivers#Capabilities
		SendSignals: true,
		Exec:        false,
		RemoteTasks: true,
	}
)

// Config contains configuration information for the plugin
type Config struct {
	Enabled bool   `codec:"enabled"`
	Runtime string `codec:"runtime"`
}

// RegistryAuth info to pull image from registry.
type RegistryAuth struct {
	Username string `codec:"username"`
	Password string `codec:"password"`
}

// TaskConfig contains configuration information for a task that runs with
// this plugin
type TaskConfig struct {
	Image   string       `codec:"image"`
	Command string       `codec:"command"`
	Auth    RegistryAuth `codec:"auth"`
	Ports   []string     `codec:"ports"`
	Copy    []string     `codec:"copy"`
	Args    []string     `codec:"args"`
}

// TaskState is the runtime state which is encoded in the handle returned to
// Nomad client.
// This information is needed to rebuild the task state and handler during
// recovery.
type TaskState struct {
	ReattachConfig *structs.ReattachConfig
	TaskConfig     *drivers.TaskConfig
	StartedAt      time.Time

	totalCpuStats  *stats.CpuStats
	userCpuStats   *stats.CpuStats
	systemCpuStats *stats.CpuStats

	// TODO: add any extra important values that must be persisted in order
	// to restore a task.
	//
	// The plugin keeps track of its running tasks in a in-memory data
	// structure. If the plugin crashes, this data will be lost, so Nomad
	// will respawn a new instance of the plugin and try to restore its
	// in-memory representation of the running tasks using the RecoverTask()
	// method below.
	Pid         int
	ContainerID string
	CHContainer CHContainer

	stateLock sync.RWMutex
}

// Driver is an example driver plugin. When provisioned in a job,
type Driver struct {
	// eventer is used to handle multiplexing of TaskEvents calls such that an
	// event can be broadcast to all callers
	eventer *eventer.Eventer

	// config is the plugin configuration set by the SetConfig RPC
	config *Config

	// nomadConfig is the client config from Nomad
	nomadConfig *base.ClientDriverConfig

	// The Docker client to interact with on the client
	dockerClient *docker.Client

	// tasks is the in memory datastore mapping taskIDs to driver handles
	tasks *taskStore

	// ctx is the context for the driver. It is passed to other subsystems to
	// coordinate shutdown
	ctx context.Context

	// signalShutdown is called when the driver is shutting down and cancels
	// the ctx passed to any subsystems
	signalShutdown context.CancelFunc

	// logger will log to the Nomad agent
	logger hclog.Logger
}

// NewPlugin returns a new example driver plugin
func NewPlugin(logger hclog.Logger) drivers.DriverPlugin {
	ctx, cancel := context.WithCancel(context.Background())
	logger = logger.Named(pluginName)

	return &Driver{
		eventer:        eventer.NewEventer(ctx, logger),
		config:         &Config{},
		tasks:          newTaskStore(),
		ctx:            ctx,
		signalShutdown: cancel,
		logger:         logger,
	}
}

// PluginInfo returns information describing the plugin.
func (d *Driver) PluginInfo() (*base.PluginInfoResponse, error) {
	return pluginInfo, nil
}

// ConfigSchema returns the plugin configuration schema.
func (d *Driver) ConfigSchema() (*hclspec.Spec, error) {
	return configSpec, nil
}

// SetConfig is called by the client to pass the configuration for the plugin.
func (d *Driver) SetConfig(cfg *base.Config) error {
	var config Config
	if len(cfg.PluginConfig) != 0 {
		if err := base.MsgPackDecode(cfg.PluginConfig, &config); err != nil {
			return err
		}
	}

	// Save the configuration to the plugin
	d.config = &config

	// TODO: parse and validated any configuration value if necessary.
	//
	// If your driver agent configuration requires any complex validation
	// (some dependency between attributes) or special data parsing (the
	// string "10s" into a time.Interval) you can do it here and update the
	// value in d.config.
	//
	// In the example below we check if the shell specified by the user is
	// supported by the plugin.
	dockerClient, err := docker.NewClientWithOpts(docker.FromEnv)
	if err != nil {
		return fmt.Errorf("Error creating client (%s): %v\n", os.Getenv("DOCKER_HOST"), err)
	}
	d.dockerClient = dockerClient

	// Save the Nomad agent configuration
	if cfg.AgentConfig != nil {
		d.nomadConfig = cfg.AgentConfig.Driver
	}

	// TODO: initialize any extra requirements if necessary.
	//
	// Here you can use the config values to initialize any resources that are
	// shared by all tasks that use this driver, such as a daemon process.

	return nil
}

// TaskConfigSchema returns the HCL schema for the configuration of a task.
func (d *Driver) TaskConfigSchema() (*hclspec.Spec, error) {
	return taskConfigSpec, nil
}

// Capabilities returns the features supported by the driver.
func (d *Driver) Capabilities() (*drivers.Capabilities, error) {
	return capabilities, nil
}

// Fingerprint returns a channel that will be used to send health information
// and other driver specific node attributes.
func (d *Driver) Fingerprint(ctx context.Context) (<-chan *drivers.Fingerprint, error) {
	ch := make(chan *drivers.Fingerprint)
	go d.handleFingerprint(ctx, ch)
	return ch, nil
}

// handleFingerprint manages the channel and the flow of fingerprint data.
func (d *Driver) handleFingerprint(ctx context.Context, ch chan<- *drivers.Fingerprint) {
	defer close(ch)

	// Nomad expects the initial fingerprint to be sent immediately
	ticker := time.NewTimer(0)
	for {
		select {
		case <-ctx.Done():
			return
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			// after the initial fingerprint we can set the proper fingerprint
			// period
			ticker.Reset(fingerprintPeriod)
			ch <- d.buildFingerprint()
		}
	}
}

// buildFingerprint returns the driver's fingerprint data
func (d *Driver) buildFingerprint() *drivers.Fingerprint {
	fp := &drivers.Fingerprint{
		Attributes:        map[string]*structs.Attribute{},
		Health:            drivers.HealthStateHealthy,
		HealthDescription: drivers.DriverHealthy,
	}

	// Fingerprinting is used by the plugin to relay two important information
	// to Nomad: health state and node attributes.
	//
	// If the plugin reports to be unhealthy, or doesn't send any fingerprint
	// data in the expected interval of time, Nomad will restart it.
	//
	// Node attributes can be used to report any relevant information about
	// the node in which the plugin is running (specific library availability,
	// installed versions of a software etc.). These attributes can then be
	// used by an operator to set job constrains.
	//
	// In the example below we check if the shell specified by the user exists
	// in the node.
	fp.Attributes["driver.ch.effective_uid"] = structs.NewIntAttribute(int64(unix.Geteuid()), "")

	clientVersion := "unknown"
	nrContainers := 0
	if d.dockerClient != nil {
		clientVersion = d.dockerClient.ClientVersion()
		if l, err := d.dockerClient.ContainerList(d.ctx, types.ContainerListOptions{}); err == nil {
			nrContainers = len(l)
		}
	}
	fp.Attributes["driver.ch.docker_client_version"] = structs.NewStringAttribute(clientVersion)
	fp.Attributes["driver.ch.container_count"] = structs.NewIntAttribute(int64(nrContainers), "")
	fp.Attributes["driver.ch.runtime"] = structs.NewStringAttribute(d.config.Runtime)
	return fp
}

// StartTask returns a task handle and a driver network if necessary.
func (d *Driver) StartTask(cfg *drivers.TaskConfig) (*drivers.TaskHandle, *drivers.DriverNetwork, error) {
	if _, ok := d.tasks.Get(cfg.ID); ok {
		return nil, nil, fmt.Errorf("task with ID %q already started", cfg.ID)
	}

	var driverConfig TaskConfig
	if err := cfg.DecodeDriverConfig(&driverConfig); err != nil {
		return nil, nil, fmt.Errorf("failed to decode driver config: %v", err)
	}

	d.logger.Info("starting ch task", "driver_cfg", hclog.Fmt("%+v", driverConfig))
	handle := drivers.NewTaskHandle(taskHandleVersion)
	handle.Config = cfg

	c, err := d.initializeContainer(cfg, driverConfig)
	if err != nil {
		return nil, nil, err
	}
	cleanup := func() {
		if err := d.dockerClient.ContainerRemove(d.ctx, c.CreateBody.ID, types.ContainerRemoveOptions{
			RemoveLinks:   true,
			RemoveVolumes: true,
			Force:         true,
		}); err != nil {
			d.logger.Error("failed to clean up from an error in Start", "error", err)
		}
	}
	if err := d.dockerClient.ContainerStart(d.ctx, c.CreateBody.ID, types.ContainerStartOptions{}); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("unable to start container '%s': %v", c.CreateBody.ID, err)
	}

	// dlogger
	dlogger, pluginClient, err := d.setupNewDockerLogger(c.CreateBody.ID, cfg, time.Unix(0, 0))
	if err != nil {
		d.logger.Error("an error occurred after container startup, terminating container", "container_id", c.CreateBody.ID)
		_ = d.dockerClient.ContainerStop(d.ctx, c.CreateBody.ID, nil)
		return nil, nil, err
	}

	// Detect container address
	ip, autoUse := d.detectIP(c)
	net := &drivers.DriverNetwork{
		//PortMap:       driverConfig.PortMap,
		IP:            ip,
		AutoAdvertise: autoUse,
	}

	h := &taskHandle{
		chContainer:         *c,
		containerID:         c.CreateBody.ID,
		taskConfig:          cfg,
		dockerClient:        d.dockerClient,
		procState:           drivers.TaskStateRunning,
		startedAt:           time.Now().Round(time.Millisecond),
		logger:              d.logger,
		dlogger:             dlogger,
		dloggerPluginClient: pluginClient,
		net:                 net,

		totalCpuStats:  stats.NewCpuStats(),
		userCpuStats:   stats.NewCpuStats(),
		systemCpuStats: stats.NewCpuStats(),
	}

	driverState := TaskState{
		ContainerID: c.CreateBody.ID,
		CHContainer: *c,
		TaskConfig:  cfg,
		StartedAt:   h.startedAt,
	}

	if err := handle.SetDriverState(&driverState); err != nil {
		return nil, nil, fmt.Errorf("failed to set driver state: %v", err)
	}
	// Mirror connections
	if listeners, err := d.setupMirrorListeners(handle, ip); err != nil {
		return nil, nil, fmt.Errorf("failed to set up listeners: %w", err)
	} else {
		h.listeners = listeners
	}

	d.tasks.Set(cfg.ID, h)
	go h.run()
	return handle, net, nil
}

// RecoverTask recreates the in-memory state of a task from a TaskHandle.
func (d *Driver) RecoverTask(handle *drivers.TaskHandle) error {
	if handle == nil {
		return errors.New("error: handle cannot be nil")
	}

	if _, ok := d.tasks.Get(handle.Config.ID); ok {
		return nil
	}

	var taskState TaskState
	if err := handle.GetDriverState(&taskState); err != nil {
		return fmt.Errorf("failed to decode task state from handle: %v", err)
	}

	var driverConfig TaskConfig
	if err := taskState.TaskConfig.DecodeDriverConfig(&driverConfig); err != nil {
		return fmt.Errorf("failed to decode driver config: %v", err)
	}

	if err := d.dockerClient.ContainerStart(d.ctx, taskState.ContainerID, types.ContainerStartOptions{}); err != nil {
		return fmt.Errorf("failed to start task from handle: %v", err)
	}

	// Detect container address
	ip, autoUse := d.detectIP(&taskState.CHContainer)
	net := &drivers.DriverNetwork{
		//PortMap:       driverConfig.PortMap,
		IP:            ip,
		AutoAdvertise: autoUse,
	}

	h := &taskHandle{
		chContainer: taskState.CHContainer,
		containerID: taskState.ContainerID,
		taskConfig:  taskState.TaskConfig,
		procState:   drivers.TaskStateRunning,
		startedAt:   taskState.StartedAt,
		exitResult:  &drivers.ExitResult{},
		net:         net,

		totalCpuStats:  stats.NewCpuStats(),
		userCpuStats:   stats.NewCpuStats(),
		systemCpuStats: stats.NewCpuStats(),
	}

	// TODO: check if reattach logic is really correct. We may need to get it from the dockerlog Plugin (GetDriverState)

	// dlogger
	var err error
	h.dlogger, h.dloggerPluginClient, err = d.reattachToDockerLogger(taskState.ReattachConfig)
	if err != nil {
		d.logger.Warn("failed to reattach to docker logger process", "error", err)

		h.dlogger, h.dloggerPluginClient, err = d.setupNewDockerLogger(h.containerID, handle.Config, time.Now())
		if err != nil {
			if err := d.dockerClient.ContainerStop(d.ctx, h.containerID, nil); err != nil {
				d.logger.Warn("failed to stop container during cleanup", "container_id", h.containerID, "error", err)
			}
			return fmt.Errorf("failed to setup replacement docker logger: %v", err)
		}

		if err := handle.SetDriverState(h.buildState()); err != nil {
			if err := d.dockerClient.ContainerStop(d.ctx, h.containerID, nil); err != nil {
				d.logger.Warn("failed to stop container during cleanup", "container_id", h.containerID, "error", err)
			}
			return fmt.Errorf("failed to store driver state: %v", err)
		}
	}
	// Mirror connections
	if listeners, err := d.setupMirrorListeners(handle, ip); err != nil {
		return fmt.Errorf("failed to set up listeners: %w", err)
	} else {
		h.listeners = listeners
	}

	d.tasks.Set(taskState.TaskConfig.ID, h)

	go h.run()
	return nil
}

// WaitTask returns a channel used to notify Nomad when a task exits.
func (d *Driver) WaitTask(ctx context.Context, taskID string) (<-chan *drivers.ExitResult, error) {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	ch := make(chan *drivers.ExitResult)
	go d.handleWait(ctx, handle, ch)
	return ch, nil
}

func (d *Driver) handleWait(ctx context.Context, handle *taskHandle, ch chan *drivers.ExitResult) {
	defer close(ch)

	//
	// Wait for process completion by polling status from handler.
	// We cannot use the following alternatives:
	//   * Process.Wait() requires LXC container processes to be children
	//     of self process; but LXC runs container in separate PID hierarchy
	//     owned by PID 1.
	//   * lxc.Container.Wait() holds a write lock on container and prevents
	//     any other calls, including stats.
	//
	// Going with the simplest approach of polling for handler to mark exit.
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-d.ctx.Done():
			return
		case <-ticker.C:
			s := handle.TaskStatus()
			if s.State == drivers.TaskStateExited {
				ch <- handle.exitResult
			}
		}
	}
}

// StopTask stops a running task with the given signal and within the timeout window.
func (d *Driver) StopTask(taskID string, timeout time.Duration, signal string) error {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}

	return d.dockerClient.ContainerStop(d.ctx, handle.containerID, nil)
}

// DestroyTask cleans up and removes a task that has terminated.
func (d *Driver) DestroyTask(taskID string, force bool) error {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}

	if handle.IsRunning() && !force {
		return errors.New("cannot destroy running task")
	}

	if err := d.dockerClient.ContainerRemove(d.ctx, handle.containerID, types.ContainerRemoveOptions{}); err != nil {
		handle.logger.Error("failed to destroy ch container", "err", err)
	}

	d.tasks.Delete(taskID)
	return nil
}

// InspectTask returns detailed status information for the referenced taskID.
func (d *Driver) InspectTask(taskID string) (*drivers.TaskStatus, error) {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	return handle.TaskStatus(), nil
}

// TaskStats returns a channel which the driver should send stats to at the given interval.
func (d *Driver) TaskStats(ctx context.Context, taskID string, interval time.Duration) (<-chan *drivers.TaskResourceUsage, error) {
	handle, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	return handle.stats(ctx, interval)
}

// TaskEvents returns a channel that the plugin can use to emit task related events.
func (d *Driver) TaskEvents(ctx context.Context) (<-chan *drivers.TaskEvent, error) {
	return d.eventer.TaskEvents(ctx)
}

// SignalTask forwards a signal to a task.
// This is an optional capability.
func (d *Driver) SignalTask(taskID string, signal string) error {
	_, ok := d.tasks.Get(taskID)
	if !ok {
		return drivers.ErrTaskNotFound
	}

	// TODO: implement driver specific signal handling logic.
	//
	// The given signal must be forwarded to the target taskID. If this plugin
	// doesn't support receiving signals (capability SendSignals is set to
	// false) you can just return nil.
	return errors.New("this driver does not support signalling")
}

var _ drivers.ExecTaskStreamingDriver = (*Driver)(nil)

// ExecTask returns the result of executing the given command inside a task.
// This is an optional capability.
func (d *Driver) ExecTask(taskID string, cmd []string, timeout time.Duration) (*drivers.ExecTaskResult, error) {
	// TODO: implement driver specific logic to execute commands in a task.
	return nil, errors.New("this driver does not support exec")
}

func (d *Driver) ExecTaskStreaming(ctx context.Context, taskID string, opts *drivers.ExecOptions) (*drivers.ExitResult, error) {
	defer func() {
		_ = opts.Stdout.Close()
	}()
	defer func() {
		_ = opts.Stderr.Close()
	}()

	done := make(chan interface{})
	defer close(done)

	h, ok := d.tasks.Get(taskID)
	if !ok {
		return nil, drivers.ErrTaskNotFound
	}

	if len(opts.Command) == 0 {
		return nil, fmt.Errorf("command is required but was empty")
	}

	createExecConfig := types.ExecConfig{
		AttachStderr: true,
		AttachStdout: true,
		AttachStdin:  true,
		Tty:          opts.Tty,
		Cmd:          opts.Command,
	}

	exec, err := h.dockerClient.ContainerExecCreate(ctx, h.containerID, createExecConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create exec: %v", err)
	}

	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case <-done:
				return
			case s, ok := <-opts.ResizeCh:
				if !ok {
					return
				}
				_ = h.dockerClient.ContainerExecResize(ctx, exec.ID, types.ResizeOptions{Height: uint(s.Height), Width: uint(s.Width)})
			}
		}
	}()

	containerConn, err := h.dockerClient.ContainerExecAttach(ctx, exec.ID, types.ExecStartCheck{
		Detach: false,
		Tty:    opts.Tty,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to attach: %w", err)
	}
	err = d.execPipe(containerConn, opts.Stdin, opts.Stdout, opts.Stderr)
	if err != nil {
		return nil, fmt.Errorf("error in pipe: %w", err)
	}

	const execTerminatingTimeout = 3 * time.Second
	start := time.Now()
	var res types.ContainerExecInspect
	for (res.Running) && time.Since(start) <= execTerminatingTimeout {
		res, err = h.dockerClient.ContainerExecInspect(ctx, exec.ID)
		if err != nil {
			return nil, fmt.Errorf("failed to inspect exec result: %w", err)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if res.Running {
		return nil, fmt.Errorf("failed to retrieve exec result")
	}
	return &drivers.ExitResult{
		ExitCode: res.ExitCode,
	}, nil
}

func (d *Driver) execPipe(containerConn types.HijackedResponse, inStream io.Reader, outStream, errorStream io.Writer) error {
	var err error
	receiveStdout := make(chan error, 1)
	if outStream != nil || errorStream != nil {
		go func() {
			// always do this because we are never tty
			//_, err = stdcopy.StdCopy(outStream, errorStream, containerConn.Reader)
			_, err = io.Copy(outStream, containerConn.Reader)
			d.logger.Debug("[hijack] End of stdout")
			receiveStdout <- err
		}()
	}

	stdinDone := make(chan struct{})
	go func() {
		if inStream != nil {
			_, _ = io.Copy(containerConn.Conn, inStream)
			d.logger.Debug("[hijack] End of stdin")
		}

		if err := containerConn.CloseWrite(); err != nil {
			d.logger.Error("couldn't send EOF", "error", err)
		}
		close(stdinDone)
	}()

	select {
	case err := <-receiveStdout:
		if err != nil {
			d.logger.Debug("error receiveStdout", "error", err)
			return err
		}
	case <-stdinDone:
		if outStream != nil || errorStream != nil {
			if err := <-receiveStdout; err != nil {
				d.logger.Debug("error receiveStdout", "error", err)
				return err
			}
		}
	}

	return nil
}

func (d *Driver) setupNewDockerLogger(containerID string, cfg *drivers.TaskConfig, startTime time.Time) (docklog.DockerLogger, *plugin.Client, error) {
	dlogger, pluginClient, err := docklog.LaunchDockerLogger(d.logger)
	if err != nil {
		if pluginClient != nil {
			pluginClient.Kill()
		}
		return nil, nil, fmt.Errorf("failed to launch docker logger plugin: %v", err)
	}

	if err := dlogger.Start(&docklog.StartOpts{
		ContainerID: containerID,
		TTY:         false,
		Stdout:      cfg.StdoutPath,
		Stderr:      cfg.StderrPath,
		StartTime:   startTime.Unix(),
	}); err != nil {
		pluginClient.Kill()
		return nil, nil, fmt.Errorf("failed to launch docker logger process %s: %v", containerID, err)
	}

	return dlogger, pluginClient, nil
}

func (d *Driver) reattachToDockerLogger(reattachConfig *structs.ReattachConfig) (docklog.DockerLogger, *plugin.Client, error) {
	reattach, err := structs.ReattachConfigToGoPlugin(reattachConfig)
	if err != nil {
		return nil, nil, err
	}

	dlogger, dloggerPluginClient, err := docklog.ReattachDockerLogger(reattach)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to reattach to docker logger process: %v", err)
	}

	return dlogger, dloggerPluginClient, nil
}

// detectIP of Docker container. Returns the first IP found as well as true if
// the IP should be advertised (bridge network IPs return false). Returns an
// empty string and false if no IP could be found.
func (d *Driver) detectIP(c *CHContainer) (string, bool) {

	inspect, err := d.dockerClient.ContainerInspect(d.ctx, c.CreateBody.ID)
	if err != nil {
		d.logger.Error("failed to inspect container while trying to detect IP", "container_id", c.CreateBody.ID)
		return "", false
	}

	ip, ipName := "", ""
	for name, net := range inspect.NetworkSettings.Networks {
		if net.IPAddress == "" {
			// Ignore networks without an IP address
			continue
		}
		ip = net.IPAddress
		ipName = name
		break
	}

	if n := len(inspect.NetworkSettings.Networks); n > 1 {
		d.logger.Warn("multiple Docker networks for container found but Nomad only supports 1",
			"total_networks", n,
			"container_id", c.CreateBody.ID,
			"container_network", ipName)
	}
	d.logger.Info("IP detection complete", "ip_address", ip)

	return ip, true // For Container Host, always auto advertise for now
}

func (d *Driver) setupMirrorListeners(handle *drivers.TaskHandle, containerIP string) ([]chan bool, error) {
	var listeners []chan bool
	if handle.Config.Resources.Ports == nil || len(*handle.Config.Resources.Ports) == 0 {
		return listeners, nil
	}
	cleanup := func() {
		for _, x := range listeners {
			x <- true
		}
	}
	for _, p := range *handle.Config.Resources.Ports {
		localServerHost := fmt.Sprintf("%s:%d", p.HostIP, p.Value)
		remoteServerHost := fmt.Sprintf("%s:%d", containerIP, p.Value)
		d.logger.Info("starting listener", "local", hclog.Fmt("%+v", localServerHost), "remote", hclog.Fmt("%+v", remoteServerHost))
		doneChan, err := forwarder.Start(d.logger, localServerHost, remoteServerHost)
		if err != nil {
			d.logger.Error("error starting listener", "error", hclog.Fmt("%+v", err), "port", hclog.Fmt("%+v", p.Value))
			cleanup()
			return []chan bool{}, nil // Don't error out for now
		}
		listeners = append(listeners, doneChan)
	}
	return listeners, nil
}
