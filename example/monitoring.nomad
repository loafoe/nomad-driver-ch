job "metrics" {
  datacenters = ["dc1"]

  group "grafana" {
        network {
            port "http" {}
        }

        task "dashboard" {
            driver = "ch"

            service {
                tags = [
                  "${node.unique.name}-urlprefix-grafana-finer-lark.eu-west.philips-healthsuite.com/",
                  "${node.unique.name}-urlprefix-grafana-finer-lark.eu-west.philips-healthsuite.com:4443/"
                ]
                address_mode = "host"
                name = "grafana"
                port = "http"
                check {
          	  type     = "tcp"
                  port     = "http"
                  interval = "10s"
                  timeout  = "2s"
                }
            }
            env {
                GF_SERVER_HTTP_PORT = "${NOMAD_PORT_http}"
            }
            config {
                image = "grafana/grafana:8.3.3"
            }
        }
    }
}
