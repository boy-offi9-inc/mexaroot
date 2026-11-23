//go:build windows

package main

import (
	"log"

	"golang.org/x/sys/windows/svc"
)

// Run starts the daemon. Launched by the service control manager, the process
// must perform the svc protocol handshake or the SCM kills it after about
// thirty seconds with error 1053; launched from a terminal it behaves as a
// plain console program exactly as on other platforms.
func Run() {
	isService, err := svc.IsWindowsService()
	if err != nil {
		log.Fatalf("mexarootd: determine service context: %v", err)
	}
	if !isService {
		main()
		return
	}
	if err := svc.Run("mexarootd", &serviceHandler{}); err != nil {
		log.Fatalf("mexarootd: service run: %v", err)
	}
}

type serviceHandler struct{}

func (h *serviceHandler) Execute(_ []string, req <-chan svc.ChangeRequest, status chan<- svc.Status) (bool, uint32) {
	status <- svc.Status{State: svc.StartPending}

	// The service context carries no signals: cancellation and shutdown are
	// driven by the SCM requests handled below.
	exit := make(chan error, 1)
	go func() { exit <- mainWithError() }()

	status <- svc.Status{State: svc.Running, Accepts: svc.AcceptStop | svc.AcceptShutdown}

	for {
		select {
		case err := <-exit:
			return false, exitCode(err)
		case c := <-req:
			switch c.Cmd {
			case svc.Interrogate:
				status <- c.CurrentStatus
			case svc.Stop, svc.Shutdown:
				status <- svc.Status{State: svc.StopPending}
				if err := stopServe(); err != nil {
					return false, 1
				}
				return false, exitCode(<-exit)
			}
		}
	}
}

func exitCode(err error) uint32 {
	if err != nil {
		return 1
	}
	return 0
}
