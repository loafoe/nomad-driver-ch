job "prometheus" {
  datacenters = ["dc1", "dc2"]
  type        = "service"

  group "monitoring" {
    count = 1

    network {
      port "prometheus_ui" {
        static = 9090
        to     = 9090
      }
      port "grafana" {}
    }

    restart {
      attempts = 2
      interval = "30m"
      delay    = "15s"
      mode     = "fail"
    }

    ephemeral_disk {
      size = 300
    }

    task "dashboard" {
      driver = "ch"

      service {
        tags = [
          "${node.unique.name}-urlprefix-grafana-${HOSTNAME_POSTFIX}/",
          "${node.unique.name}-urlprefix-grafana-${HOSTNAME_POSTFIX}:4443/"
        ]
        address_mode = "host"
        name         = "grafana"
        port         = "grafana"
        check {
          type     = "tcp"
          port     = "grafana"
          interval = "10s"
          timeout  = "2s"
        }
      }
      env {
        GF_SERVER_HTTP_PORT = NOMAD_PORT_grafana
      }
      config {
        image = "grafana/grafana:8.3.3"
      }
    }


    task "prometheus" {
      template {
        change_mode = "noop"
        destination = "local/prometheus.yml"

        data = <<EOH
---
global:
  scrape_interval:     5s
  evaluation_interval: 5s

scrape_configs:

  - job_name: 'nomad_metrics'

    consul_sd_configs:
    - server: '{{ env "CONSUL_REGISTRY_ADDR" }}'
      services: ['nomad-client', 'nomad']

    relabel_configs:
    - source_labels: ['__meta_consul_tags']
      regex: '(.*)http(.*)'
      action: keep

    scrape_interval: 5s
    metrics_path: /v1/metrics
    params:
      format: ['prometheus']

  - job_name : 'generic_metrics'
    consul_sd_configs:
    - server: '{{ env "CONSUL_REGISTRY_ADDR" }}'

    relabel_configs:
    - source_labels: ['__meta_consul_tags']
      regex: '(.*)metrics(.*)'
      action: keep

    scrape_interval: 5s
    metrics_path: /metrics
    params:
      format: ['prometheus']
EOH
      }

      driver = "ch"

      config {
        image = "prom/prometheus:latest"
        args = [
          "--config.file=/local/prometheus.yml",
          "--enable-feature=remote-write-receiver"
        ]
        mapping = [
          "shared:/prometheus"
        ]

        ports = ["prometheus_ui"]
      }

      service {
        name         = "prometheus"
        tags         = ["${node.unique.name}-urlprefix-/"]
        port         = "prometheus_ui"
        address_mode = "host"

        check {
          name     = "prometheus_ui port alive"
          type     = "http"
          path     = "/-/healthy"
          interval = "10s"
          timeout  = "2s"
        }
      }
    }
  }
}

