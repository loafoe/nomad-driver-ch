package mirror

import (
	"io"
	"net"

	"github.com/hashicorp/go-hclog"
)

func Start(logger hclog.Logger, localServerHost, remoteServerHost string) (chan bool, error) {
	ln, err := net.Listen("tcp", localServerHost)
	if err != nil {
		return nil, err
	}

	logger.Info("port forwarding server up", "local_server", localServerHost, "remote_server", remoteServerHost)
	done := make(chan bool)

	go func() {
		go func() { // Out of band close listener
			select {
			case <-done:
				logger.Info("closing listener")
				_ = ln.Close()
				return
			}
		}()
		for {
			conn, err := ln.Accept()
			if err != nil {
				logger.Error("accept call failed, stopping forwarding server", "error", err.Error())
				return
			}
			go func() {
				_ = handleConnection(logger, remoteServerHost, conn)
			}()
		}
	}()

	return done, nil
}

func forward(src, dest net.Conn) {
	defer src.Close()
	defer dest.Close()
	_, _ = io.Copy(src, dest)
}

func handleConnection(logger hclog.Logger, remoteServerHost string, c net.Conn) error {

	logger.Info("handling connection", "from", c.RemoteAddr())

	remote, err := net.Dial("tcp", remoteServerHost)
	if err != nil {
		return err
	}

	logger.Info("connection established", "to", remoteServerHost)

	// go routines to initiate bidirectional communication for local server with a
	// remote server
	go forward(c, remote)
	go forward(remote, c)

	return nil
}
