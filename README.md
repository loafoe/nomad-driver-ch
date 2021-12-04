# Nomad Container Host (ch) Driver Plugin

This is a [Nomad](https://www.nomadproject.io) Driver plugin that allows
HSDP Container Host instances to function as Nomad client nodes. 

The initial focus focus is on getting things working on a single client.
Once this is stable we will expand the scope of the project to cluster
setups and possibly enabling auto scaling features.

The primary goal of this project is to build knowledge of Nomad and its
internals and to validate Nomad as a possible alternative to Kubernetes
which is much more complex and heavy weight. 

## Limitations and aspirations

The current Container Host architecture rules out any sort of multi-tenancy
capability of the Nomad cluster so any deployment using this driver 
is effectively single tenant today. This is fine as there is very little overhead.
Customers can theoretically spin up dozens of clusters. 
Even though the driver is Container Host specific, any knowledge we gain should be
applicable to any future hardened environment. Potentially it can also be 
an interesting platform for on-premise or hybrid deployments.

# Requirements

- [Nomad](https://www.nomadproject.io/downloads.html) v1.1+
- [Go](https://golang.org/doc/install) v1.17 or later (to build the plugin)

# Building the Plugin

Clone the repository somewhere in your computer. This project uses
[Go modules](https://blog.golang.org/using-go-modules) so you will need to set
the environment variable `GO111MODULE=on` or work outside your `GOPATH` if it
is set to `auto` or not declared.

```sh
$ git clone https://github.com/loafoe/nomad-ch-driver

Enter the plugin directory and update the paths in `go.mod` and `main.go` to
match your repository path.

Build the plugin.

```sh
$ go build .
```

## Deploying Driver Plugins in Nomad

```sh
$ nomad agent -dev -config=./example/agent.hcl -plugin-dir=$(pwd)

# in another shell
$ nomad run ./example/example.nomad
$ nomad logs <ALLOCATION ID>
```
