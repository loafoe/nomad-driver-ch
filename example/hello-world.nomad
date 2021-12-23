job "hello-world" {
  datacenters = ["dc1", "dc2"]

  group "hello-world" {
    count = 4
    
    network {
    	port "http" {}
    }

    task "hello-world" {
      driver = "ch"

      service {
         name = "hello-world-metrics"
         port = "9001"
         tags = ["metrics"]
         address_mode = "driver"
      }

      service {
         name = "hello-world"
         tags = [
                "${node.unique.name}-urlprefix-test-${HOSTNAME_POSTFIX}/",
                "${node.unique.name}-urlprefix-test-${HOSTNAME_POSTFIX}:4443/"
	 ]
         port = "8080"

	 address_mode = "driver"

         meta {
           meta = "for your service"
         }
         check {
          type     = "tcp"
          port     = "http"
          interval = "10s"
          timeout  = "2s"
        }
      }
     
      env {
        PORT = "8080"
      }
 
      config {
        image = "loafoe/go-hello-world:v0.4.0"
        ports = ["http"]
      }

      resources {
        cpu    = 500
        memory = 128
      }
    }
  }
}
