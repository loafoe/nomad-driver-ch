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

func (d *CHDriverPlugin) generateAuth(auth *RegistryAuth) string {
	if auth == nil || auth.Username == "" {
		return ""
	}
	str := auth.Username + ":" + auth.Password
	return base64.StdEncoding.EncodeToString([]byte(str))
}

func (d *CHDriverPlugin) initializeContainer(cfg *drivers.TaskConfig, taskConfig TaskConfig) (*container.ContainerCreateCreatedBody, error) {
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
		io.Copy(os.Stdout, reader)
	}
	mounts, err := d.mountEntries(d.ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("error setting up volume mounts: %w", err)
	}

	config := &container.Config{
		Image: taskConfig.Image,
	}
	hostConfig := &container.HostConfig{
		Mounts: *mounts,
	}
	networkingConfig := &network.NetworkingConfig{}
	platform := &v1.Platform{
		Architecture: "amd64", // Hardcoded until we figure out how to configure this on-the-fly
		OS:           "linux",
	}

	d.logger.Info("creating container", "container_name", containerName)
	body, err := d.dockerClient.ContainerCreate(d.ctx, config, hostConfig, networkingConfig, platform, containerName)
	if err != nil {
		return nil, fmt.Errorf("eror in containerCreate: %w", err)
	}
	return &body, nil
}

func (d *CHDriverPlugin) mountEntries(ctx context.Context, cfg *drivers.TaskConfig) (*[]mount.Mount, error) {
	var mounts []mount.Mount

	cleanup := func() {
		for _, m := range mounts {
			_ = d.dockerClient.VolumeRemove(ctx, m.Source, true)
		}
		return
	}

	mapper := []struct {
		Name       string
		Source     string
		MountPoint string
	}{
		{"local", cfg.TaskDir().LocalDir, "/local"},
		{"shared", cfg.TaskDir().SharedTaskDir, "/shared"},
		{"secrets", cfg.TaskDir().SecretsDir, "/secrets"},
	}
	for _, m := range mapper {
		localName := fmt.Sprintf("%s-%s", cfg.ID, m.Name)
		_, err := d.dockerClient.VolumeCreate(ctx, volumetypes.VolumeCreateBody{
			Driver:     "local",
			Name:       localName,
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
		mounts = append(mounts, mount.Mount{
			Type:   mount.TypeVolume,
			Source: localName,
			Target: m.MountPoint,
		})
	}
	return &mounts, nil
}
