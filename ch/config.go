package ch

import (
	"time"

	"github.com/hashicorp/nomad/plugins/base"
	"github.com/hashicorp/nomad/plugins/drivers"
	"github.com/hashicorp/nomad/plugins/shared/hclspec"
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
		"image":      hclspec.NewAttr("image", "string", true),
		"command":    hclspec.NewAttr("command", "string", false),
		"entrypoint": hclspec.NewAttr("entrypoint", "string", false),
		"ports":      hclspec.NewAttr("ports", "list(string)", false),
		"auth": hclspec.NewBlock("auth", false, hclspec.NewObject(map[string]*hclspec.Spec{
			"username": hclspec.NewAttr("username", "string", true),
			"password": hclspec.NewAttr("password", "string", true),
		})),
		"copy":   hclspec.NewAttr("copy", "list(string)", false),
		"args":   hclspec.NewAttr("args", "list(string)", false),
		"mounts": hclspec.NewAttr("mounts", "list(string)", false),
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
	Image      string       `codec:"image"`
	Command    string       `codec:"command"`
	Entrypoint []string     `codec:"entrypoint"`
	Auth       RegistryAuth `codec:"auth"`
	Ports      []string     `codec:"ports"`
	Copy       []string     `codec:"copy"`
	Args       []string     `codec:"args"`
	Mounts     []string     `codec:"mounts"`
}
