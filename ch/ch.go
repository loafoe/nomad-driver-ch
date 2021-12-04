package ch

import (
	"encoding/base64"
	"fmt"
	"io"
	"os"

	"github.com/docker/docker/api/types"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/hashicorp/nomad/plugins/drivers"
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

	opt := types.ImagePullOptions{
		RegistryAuth: d.generateAuth(&taskConfig.Auth),
	}
	d.logger.Info("pulling image", "image", taskConfig.Image)

	reader, err := d.dockerClient.ImagePull(d.ctx, taskConfig.Image, opt)
	if err != nil {
		return nil, fmt.Errorf("error pulling image '%s': %w", taskConfig.Image, err)
	}
	defer reader.Close()
	io.Copy(os.Stdout, reader)

	config := &container.Config{
		Image: taskConfig.Image,
	}
	hostConfig := &container.HostConfig{}
	networkingConfig := &network.NetworkingConfig{}

	d.logger.Info("creating container", "container_name", containerName)
	body, err := d.dockerClient.ContainerCreate(d.ctx, config, hostConfig, networkingConfig, containerName)
	if err != nil {
		return nil, fmt.Errorf("eror in containerCreate: %w", err)
	}
	return &body, nil
}
