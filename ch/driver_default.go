//go:build !windows
// +build !windows

package ch

import (
	"github.com/docker/go-connections/nat"
)

func getPortBinding(ip string, port string) nat.PortBinding {
	return nat.PortBinding{HostIP: ip, HostPort: port}
}
