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
	"bytes"
	"context"
	_ "embed"
	"encoding/base64"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"runtime"
	"strings"

	"github.com/docker/distribution/reference"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	volumetypes "github.com/docker/docker/api/types/volume"
	docker "github.com/docker/docker/client"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/uuid"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/opencontainers/image-spec/specs-go/v1"
)

//go:embed nomad-copier-amd64.tar
var nomadCopierAMD64 []byte

//go:embed nomad-copier-arm64.tar
var nomadCopierARM64 []byte

func (d *Driver) generateAuth(auth *RegistryAuth) string {
	if auth == nil || auth.Username == "" {
		return ""
	}
	str := auth.Username + ":" + auth.Password
	return base64.StdEncoding.EncodeToString([]byte(str))
}

func (d *Driver) initializeContainer(cfg *drivers.TaskConfig, taskConfig TaskConfig) (*CHContainer, error) {
	chContainer := CHContainer{}

	containerName := fmt.Sprintf("%s-%s", cfg.Name, cfg.AllocID)
	opt := types.ImagePullOptions{}
	ref, err := reference.ParseNormalizedNamed(taskConfig.Image)
	if err != nil {
		return nil, fmt.Errorf("error parsing image: %w", err)
	}
	serverAddress := ""
	if str := strings.Split(ref.Name(), "/"); len(str) > 1 {
		serverAddress = str[0]
	}
	d.logger.Info("pulling image", "image", taskConfig.Image, "username", taskConfig.Auth.Username)
	if taskConfig.Auth.Username != "" {
		image, err := crane.Pull(taskConfig.Image, crane.WithAuth(&authn.Basic{
			Username: taskConfig.Auth.Username,
			Password: taskConfig.Auth.Password,
		}))
		if err != nil {
			return nil, fmt.Errorf("error pulling image from registry '%s': %w", serverAddress, err)
		}
		dest, err := os.CreateTemp("", "image")
		if err != nil {
			return nil, fmt.Errorf("cannot open local file for storing image: %w", err)
		}
		defer func() {
			_ = dest.Close()
		}()
		err = crane.Save(image, taskConfig.Image, dest.Name())
		if err != nil {
			return nil, fmt.Errorf("error saving image to '%s': %w", dest.Name(), err)
		}
		lr, err := d.dockerClient.ImageLoad(d.ctx, dest, false)
		if err != nil {
			return nil, fmt.Errorf("error loading image: %w", err)
		}
		defer func() {
			_ = lr.Body.Close()
		}()
	} else {
		reader, err := d.dockerClient.ImagePull(d.ctx, taskConfig.Image, opt)
		if err != nil {
			return nil, fmt.Errorf("error pulling image '%s': %w", taskConfig.Image, err)
		}
		defer func() {
			_ = reader.Close()
		}()
		_, _ = io.Copy(os.Stdout, reader)
	}
	mountEntries, err := d.mountEntries(d.ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("error setting up volume mounts: %w", err)
	}
	chContainer.Mounts = *mountEntries

	config := &container.Config{
		Image: taskConfig.Image,
	}

	var mounts []mount.Mount
	for _, m := range *mountEntries {
		mounts = append(mounts, m.Mount)
	}
	hostConfig := &container.HostConfig{
		Runtime: d.config.Runtime,
		Mounts:  mounts,
	}
	// Set limits
	hostConfig.Resources.Memory = cfg.Resources.NomadResources.Memory.MemoryMB * 1000 * 1000
	hostConfig.Resources.CPUCount = int64(runtime.NumCPU())
	hostConfig.Resources.CPUShares = cfg.Resources.NomadResources.Cpu.CpuShares

	// Networking and Ports
	networkingConfig := &network.NetworkingConfig{}

	ports := newPublishedPorts(d.logger)
	if cfg.Resources.Ports != nil && len(taskConfig.Ports) > 0 {
		for _, port := range taskConfig.Ports {
			if mapping, ok := cfg.Resources.Ports.Get(port); ok {
				ports.add(mapping.Label, "0.0.0.0", mapping.Value, mapping.To) // mapping.HostIP
			} else {
				return nil, fmt.Errorf("port %q not found, check network stanza", port)
			}
		}
	}
	hostConfig.PortBindings = ports.publishedPorts
	config.ExposedPorts = ports.exposedPorts

	platform := &v1.Platform{
		Architecture: runtime.GOARCH,
		OS:           "linux",
	}

	// Set environment variables
	for key, val := range cfg.Env {
		if skipOverride(key) {
			continue
		}
		config.Env = append(config.Env, fmt.Sprintf("%s=%s", key, val))
	}

	// Set command
	if taskConfig.Command != "" {
		// Validate command
		if err := validateCommand(taskConfig.Command); err != nil {
			return nil, err
		}

		cmd := []string{taskConfig.Command}
		if len(taskConfig.Args) != 0 {
			cmd = append(cmd, taskConfig.Args...)
		}
		d.logger.Debug("setting container startup command", "command", strings.Join(cmd, " "))
		config.Cmd = cmd
	} else if len(taskConfig.Args) != 0 {
		config.Cmd = taskConfig.Args
	}

	d.logger.Info("creating container", "container_name", containerName)
	body, err := d.dockerClient.ContainerCreate(d.ctx, config, hostConfig, networkingConfig, platform, containerName)
	if err != nil {
		return nil, fmt.Errorf("eror in containerCreate: %w", err)
	}
	chContainer.CreateBody = body

	// local copy to container
	err = localToContainer(d.dockerClient, body.ID, cfg, taskConfig.Copy)
	if err != nil {
		_ = d.dockerClient.ContainerRemove(d.ctx, body.ID, types.ContainerRemoveOptions{Force: true})
		return nil, err
	}

	return &chContainer, nil
}

type CHContainer struct {
	CreateBody container.ContainerCreateCreatedBody
	Mounts     []CHMount
}

type CHMount struct {
	mount.Mount
	Name       string
	Source     string
	MountPoint string
	Volume     string
	Sync       bool
}

func (d *Driver) mountEntries(ctx context.Context, cfg *drivers.TaskConfig) (*[]CHMount, error) {
	var mounts []CHMount

	cleanup := func() {
		for _, m := range mounts {
			_ = d.dockerClient.VolumeRemove(ctx, m.Source, true)
		}
	}
	mapper := []CHMount{
		{Name: "local", Source: cfg.TaskDir().LocalDir, MountPoint: "/local", Volume: fmt.Sprintf("%s-local", cfg.AllocID), Sync: true},
		{Name: "shared", Source: cfg.TaskDir().SharedTaskDir, MountPoint: "/shared", Volume: fmt.Sprintf("%s-shared", cfg.TaskGroupName), Sync: false},
		{Name: "secrets", Source: cfg.TaskDir().SecretsDir, MountPoint: "/secrets", Volume: fmt.Sprintf("%s-secret", cfg.AllocID), Sync: true},
	}
	for _, m := range mapper {
		_, err := d.dockerClient.VolumeCreate(ctx, volumetypes.VolumeCreateBody{
			Driver:     "local",
			Name:       m.Volume,
			DriverOpts: map[string]string{},
			Labels: map[string]string{
				"created_by": "nomad-driver-ch",
			},
		})
		if err != nil {
			cleanup()
			return nil, err
		}
		m.Mount = mount.Mount{
			Type:   mount.TypeVolume,
			Source: m.Volume,
			Target: m.MountPoint,
		}
		mounts = append(mounts, m)
	}
	// Set up copy container
	copier := bytes.NewReader(nomadCopierAMD64)
	if runtime.GOARCH == "arm64" {
		copier = bytes.NewReader(nomadCopierARM64)
	}
	loadResp, err := d.dockerClient.ImageLoad(ctx, copier, false)
	if err != nil {
		return nil, fmt.Errorf("error loading image: %w", err)
	}
	_, _ = ioutil.ReadAll(loadResp.Body)
	_ = loadResp.Body.Close()

	var dockerMounts []mount.Mount
	for _, m := range mounts {
		dockerMounts = append(dockerMounts, m.Mount)
	}
	d.logger.Debug("-------------------- starting sync --------------------")
	copyName := fmt.Sprintf("nomad-copier-%s", uuid.NewString())
	resp, err := d.dockerClient.ContainerCreate(ctx, &container.Config{
		Cmd:   []string{"/bin/true"},
		Image: "nomad-copier",
	}, &container.HostConfig{
		Mounts: dockerMounts,
	}, &network.NetworkingConfig{}, &v1.Platform{
		Architecture: runtime.GOARCH,
		OS:           "linux",
	}, copyName)
	if err != nil {
		d.logger.Error("failed to set up copy container", "error", err.Error())
		return nil, err
	}
	defer func() {
		_ = d.dockerClient.ContainerRemove(ctx, resp.ID, types.ContainerRemoveOptions{Force: true})
	}()

	// Copy content from m.Source to new volume
	for _, m := range mounts {
		if !m.Sync {
			continue
		}
		source := fmt.Sprintf("%s/", m.Source)
		destination := fmt.Sprintf("%s:/", resp.ID)
		d.logger.Info("copying data to volumes", "source", source, "destination", destination)
		err = runCopy(d.dockerClient, copyOptions{
			source:      source,
			destination: destination,
			quiet:       true,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to sync volume: %w", err)
		}
	}
	d.logger.Debug("-------------------- done with sync --------------------")
	return &mounts, nil
}

func localToContainer(client *docker.Client, containerID string, cfg *drivers.TaskConfig, copy []string) error {
	// Copy operations to container
	for _, c := range copy {
		split := strings.Split(c, ":")
		if len(split) != 2 {
			continue
		}
		relativePath := split[0]
		containerDest := split[1]
		if !strings.HasPrefix(relativePath, relativePath) {
			return fmt.Errorf("only 'local' is supported as a copy source, requested: %s", relativePath)
		}
		source := fmt.Sprintf("%s/%s", cfg.TaskDir().LocalDir, strings.Replace(relativePath, "local/", "", 1))
		destination := fmt.Sprintf("%s:%s", containerID, containerDest)
		err := runCopy(client, copyOptions{
			source:      source,
			destination: destination,
			quiet:       true,
		})
		if err != nil {
			return fmt.Errorf("failed to copy '%s' to '%s': %w", source, destination, err)
		}
	}
	return nil
}

// validateCommand validates that the command only has a single value and
// returns a user-friendly error message telling them to use the passed
// argField.
func validateCommand(command string) error {
	trimmed := strings.TrimSpace(command)
	if len(trimmed) == 0 {
		return fmt.Errorf("command empty: %q", command)
	}

	if len(trimmed) != len(command) {
		return fmt.Errorf("command contains extra white space: %q", command)
	}

	return nil
}

// skipOverride determines whether the environment variable (key) needs an override or not.
func skipOverride(key string) bool {
	skipOverrideList := []string{"PATH", "DOCKER_HOST", "HOME", "HOSTNAME"}
	for _, k := range skipOverrideList {
		if key == k {
			return true
		}
	}
	return false
}
