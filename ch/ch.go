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
	"encoding/base64"
	"fmt"
	"io"
	"os"
	"runtime"
	"strings"

	"github.com/docker/distribution/reference"
	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/mount"
	"github.com/docker/docker/api/types/network"
	volumetypes "github.com/docker/docker/api/types/volume"
	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/opencontainers/image-spec/specs-go/v1"
)

func (d *DriverPlugin) generateAuth(auth *RegistryAuth) string {
	if auth == nil || auth.Username == "" {
		return ""
	}
	str := auth.Username + ":" + auth.Password
	return base64.StdEncoding.EncodeToString([]byte(str))
}

func (d *DriverPlugin) initializeContainer(cfg *drivers.TaskConfig, taskConfig TaskConfig) (*CHContainer, error) {
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
		defer dest.Close()
		err = crane.Save(image, taskConfig.Image, dest.Name())
		if err != nil {
			return nil, fmt.Errorf("error saving image to '%s': %w", dest.Name(), err)
		}
		lr, err := d.dockerClient.ImageLoad(d.ctx, dest, false)
		if err != nil {
			return nil, fmt.Errorf("error loading image: %w", err)
		}
		defer lr.Body.Close()
	} else {
		reader, err := d.dockerClient.ImagePull(d.ctx, taskConfig.Image, opt)
		if err != nil {
			return nil, fmt.Errorf("error pulling image '%s': %w", taskConfig.Image, err)
		}
		defer reader.Close()
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
		config.Env = append(config.Env, fmt.Sprintf("%s=%s", key, val))
	}

	// Set command
	if taskConfig.Command != "" {
		// Validate command
		if err := validateCommand(taskConfig.Command); err != nil {
			return nil, err
		}

		cmd := strings.Split(taskConfig.Command, " ")
		config.Cmd = cmd
	}

	d.logger.Info("creating container", "container_name", containerName)
	body, err := d.dockerClient.ContainerCreate(d.ctx, config, hostConfig, networkingConfig, platform, containerName)
	if err != nil {
		return nil, fmt.Errorf("eror in containerCreate: %w", err)
	}
	chContainer.CreateBody = body

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
}

func (d *DriverPlugin) mountEntries(ctx context.Context, cfg *drivers.TaskConfig) (*[]CHMount, error) {
	var mounts []CHMount

	cleanup := func() {
		for _, m := range mounts {
			_ = d.dockerClient.VolumeRemove(ctx, m.Source, true)
		}
		return
	}
	mapper := []CHMount{
		{Name: "local", Source: cfg.TaskDir().LocalDir, MountPoint: "/local", Volume: fmt.Sprintf("%s-local", cfg.AllocID)},
		{Name: "shared", Source: cfg.TaskDir().SharedTaskDir, MountPoint: "/shared", Volume: fmt.Sprintf("%s-shared", cfg.TaskGroupName)},
		{Name: "secrets", Source: cfg.TaskDir().SecretsDir, MountPoint: "/secrets", Volume: fmt.Sprintf("%s-secret", cfg.AllocID)},
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
		// TODO: copy content from m.Source to new volume
		m.Mount = mount.Mount{
			Type:   mount.TypeVolume,
			Source: m.Volume,
			Target: m.MountPoint,
		}
		mounts = append(mounts, m)
	}
	return &mounts, nil
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
