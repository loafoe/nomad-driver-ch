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
         tags = [
                "${node.unique.name}-urlprefix-test-finer-lark.eu-west.philips-healthsuite.com/",
                "${node.unique.name}-urlprefix-test-finer-lark.eu-west.philips-healthsuite.com:4443/"
	 ]
         port = "http"

	 address_mode = "host"

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
