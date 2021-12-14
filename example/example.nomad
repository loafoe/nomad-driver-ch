job "hello-world" {
  datacenters = ["dc1"]

  group "hello-world" {

    network {
    	port "http" {}
    }

    task "hello-world" {
      driver = "ch"

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
