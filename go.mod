module github.com/loafoe/nomad-ch-driver

go 1.17

replace (
	github.com/Microsoft/go-winio => github.com/endocrimes/go-winio v0.4.13-0.20190628114223-fb47a8b41948
	github.com/NYTimes/gziphandler => github.com/NYTimes/gziphandler v1.0.0
	github.com/apparentlymart/go-textseg/v12 => github.com/apparentlymart/go-textseg/v12 v12.0.0

	github.com/hashicorp/go-discover => github.com/hashicorp/go-discover v0.0.0-20210818145131-c573d69da192
	github.com/hashicorp/hcl => github.com/hashicorp/hcl v1.0.1-0.20201016140508-a07e7d50bbee
	github.com/kr/pty => github.com/kr/pty v1.1.5
)

require (
	github.com/LK4D4/joincontext v0.0.0-20171026170139-1724345da6d5 // indirect
	github.com/Microsoft/go-winio v0.4.15-0.20200113171025-3fe6c5262873 // indirect
	github.com/armon/circbuf v0.0.0-20150827004946-bbbad097214e // indirect
	github.com/armon/go-metrics v0.3.4 // indirect
	github.com/container-storage-interface/spec v1.4.0 // indirect
	github.com/containernetworking/plugins v0.7.3-0.20190501191748-2d6d46d308b2 // indirect
	github.com/docker/docker v17.12.0-ce-rc1.0.20200330121334-7f8b4b621b5d+incompatible // indirect
	github.com/fatih/color v1.9.0 // indirect
	github.com/golang/protobuf v1.5.0 // indirect
	github.com/golang/snappy v0.0.4 // indirect
	github.com/grpc-ecosystem/go-grpc-middleware v1.2.1-0.20200228141219-3ce3d519df39 // indirect
	github.com/hashicorp/consul-template v0.25.2
	github.com/hashicorp/consul/api v1.9.1 // indirect
	github.com/hashicorp/cronexpr v1.1.1 // indirect
	github.com/hashicorp/go-cleanhttp v0.5.2 // indirect
	github.com/hashicorp/go-hclog v1.0.0
	github.com/hashicorp/go-immutable-radix v1.3.0 // indirect
	github.com/hashicorp/go-msgpack v1.1.5 // indirect
	github.com/hashicorp/go-multierror v1.1.1 // indirect
	github.com/hashicorp/go-plugin v1.4.3
	github.com/hashicorp/go-sockaddr v1.0.2 // indirect
	github.com/hashicorp/go-version v1.2.1-0.20191009193637-2046c9d0f0b0 // indirect
	github.com/hashicorp/golang-lru v0.5.4 // indirect
	github.com/hashicorp/hcl v1.0.1-0.20201016140508-a07e7d50bbee // indirect
	github.com/hashicorp/raft v1.1.3-0.20200211192230-365023de17e6 // indirect
	github.com/hashicorp/serf v0.9.5 // indirect
	github.com/hashicorp/yamux v0.0.0-20181012175058-2f1d1f20f75d // indirect
	github.com/hpcloud/tail v1.0.1-0.20170814160653-37f427138745 // indirect
	github.com/kr/pty v1.1.5 // indirect
	github.com/mattn/go-colorable v0.1.7 // indirect
	github.com/miekg/dns v1.1.26 // indirect
	github.com/mitchellh/copystructure v1.1.1 // indirect
	github.com/mitchellh/go-ps v0.0.0-20190716172923-621e5597135b // indirect
	github.com/mitchellh/go-testing-interface v1.14.1 // indirect
	github.com/mitchellh/hashstructure v1.0.0 // indirect
	github.com/mitchellh/mapstructure v1.4.1 // indirect
	github.com/mitchellh/reflectwalk v1.0.1 // indirect
	github.com/opencontainers/runc v1.0.0-rc93 // indirect
	github.com/opencontainers/runtime-spec v1.0.3-0.20200929063507-e6143ca7d51d // indirect
	github.com/pkg/errors v0.9.1 // indirect
	github.com/ryanuber/go-glob v1.0.0 // indirect
	github.com/shirou/gopsutil/v3 v3.21.6-0.20210619153009-7ea8062810b6 // indirect
	github.com/syndtr/gocapability v0.0.0-20200815063812-42c35b437635 // indirect
	github.com/zclconf/go-cty v1.8.0 // indirect
	golang.org/x/crypto v0.0.0-20201002170205-7f63de1d35b0 // indirect
	golang.org/x/net v0.0.0-20210405180319-a5a99cb37ef4 // indirect
	golang.org/x/sync v0.0.0-20210220032951-036812b2e83c // indirect
	golang.org/x/sys v0.0.0-20210818153620-00dd8d7831e7 // indirect
	google.golang.org/grpc v1.29.1 // indirect
	gopkg.in/tomb.v1 v1.0.0-20141024135613-dd632973f1e7 // indirect
)

require github.com/hashicorp/nomad v1.1.6

require (
	github.com/StackExchange/wmi v0.0.0-20190523213315-cbe66965904d // indirect
	github.com/checkpoint-restore/go-criu/v4 v4.1.0 // indirect
	github.com/cilium/ebpf v0.2.0 // indirect
	github.com/containerd/console v1.0.1 // indirect
	github.com/coreos/go-systemd/v22 v22.1.0 // indirect
	github.com/cyphar/filepath-securejoin v0.2.3-0.20190205144030-7efe413b52e1 // indirect
	github.com/go-ole/go-ole v1.2.4 // indirect
	github.com/godbus/dbus/v5 v5.0.3 // indirect
	github.com/hashicorp/errwrap v1.1.0 // indirect
	github.com/hashicorp/go-rootcerts v1.0.2 // indirect
	github.com/mattn/go-isatty v0.0.12 // indirect
	github.com/mitchellh/go-homedir v1.1.0 // indirect
	github.com/moby/sys/mountinfo v0.4.0 // indirect
	github.com/mrunalp/fileutils v0.5.0 // indirect
	github.com/oklog/run v1.0.1-0.20180308005104-6934b124db28 // indirect
	github.com/opencontainers/selinux v1.8.0 // indirect
	github.com/seccomp/libseccomp-golang v0.9.2-0.20200314001724-bdab42bd5128 // indirect
	github.com/sirupsen/logrus v1.7.0 // indirect
	github.com/tklauser/go-sysconf v0.3.6 // indirect
	github.com/tklauser/numcpus v0.2.2 // indirect
	github.com/vishvananda/netlink v1.1.0 // indirect
	github.com/vishvananda/netns v0.0.0-20191106174202-0a2b9b5464df // indirect
	github.com/vmihailenco/msgpack/v4 v4.3.12 // indirect
	github.com/vmihailenco/tagparser v0.1.1 // indirect
	github.com/willf/bitset v1.1.11 // indirect
	golang.org/x/text v0.3.5 // indirect
	golang.org/x/xerrors v0.0.0-20200804184101-5ec99f83aff1 // indirect
	google.golang.org/appengine v1.6.5 // indirect
	google.golang.org/genproto v0.0.0-20200526211855-cb27e3aa2013 // indirect
	google.golang.org/protobuf v1.26.0-rc.1 // indirect
	gopkg.in/fsnotify.v1 v1.4.7 // indirect
)
