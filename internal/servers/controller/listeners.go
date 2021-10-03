package controller

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"math"
	"net"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/hashicorp/boundary/globals"
	"github.com/hashicorp/boundary/internal/cmd/base"
	pbs "github.com/hashicorp/boundary/internal/gen/controller/servers/services"
	"github.com/hashicorp/boundary/internal/libs/alpnmux"
	"github.com/hashicorp/boundary/internal/servers/controller/handlers/workers"
	"github.com/hashicorp/go-multierror"
	"google.golang.org/grpc"
	"google.golang.org/grpc/test/bufconn"
)

func (c *Controller) startListeners() error {
	servers := make([]func(), 0, len(c.conf.Listeners))

	configureForAPI := func(ln *base.ServerListener) error {
		handler, err := c.handler(HandlerProperties{
			ListenerConfig: ln.Config,
			CancelCtx:      c.baseContext,
		})
		if err != nil {
			return err
		}

		/*
			// TODO: As I write this Vault's having this code audited, make sure to
			// port over any recommendations
			//
			// We perform validation on the config earlier, we can just cast here
			if _, ok := ln.config["x_forwarded_for_authorized_addrs"]; ok {
				hopSkips := ln.config["x_forwarded_for_hop_skips"].(int)
				authzdAddrs := ln.config["x_forwarded_for_authorized_addrs"].([]*sockaddr.SockAddrMarshaler)
				rejectNotPresent := ln.config["x_forwarded_for_reject_not_present"].(bool)
				rejectNonAuthz := ln.config["x_forwarded_for_reject_not_authorized"].(bool)
				if len(authzdAddrs) > 0 {
					handler = vaulthttp.WrapForwardedForHandler(handler, authzdAddrs, rejectNotPresent, rejectNonAuthz, hopSkips)
				}
			}
		*/

		// Resolve it here to avoid race conditions if the base context is
		// replaced
		cancelCtx := c.baseContext

		server := &http.Server{
			Handler:           handler,
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			IdleTimeout:       5 * time.Minute,
			ErrorLog:          c.logger.StandardLogger(nil),
			BaseContext: func(net.Listener) context.Context {
				return cancelCtx
			},
		}
		ln.HTTPServer = server

		if ln.Config.HTTPReadHeaderTimeout > 0 {
			server.ReadHeaderTimeout = ln.Config.HTTPReadHeaderTimeout
		}
		if ln.Config.HTTPReadTimeout > 0 {
			server.ReadTimeout = ln.Config.HTTPReadTimeout
		}
		if ln.Config.HTTPWriteTimeout > 0 {
			server.WriteTimeout = ln.Config.HTTPWriteTimeout
		}
		if ln.Config.HTTPIdleTimeout > 0 {
			server.IdleTimeout = ln.Config.HTTPIdleTimeout
		}

		switch ln.Config.TLSDisable {
		case true:
			l, err := ln.Mux.RegisterProto(alpnmux.NoProto, nil)
			if err != nil {
				return fmt.Errorf("error getting non-tls listener: %w", err)
			}
			if l == nil {
				return errors.New("could not get non-tls listener")
			}
			servers = append(servers, func() {
				go server.Serve(l)
			})

		default:
			protos := []string{"", "http/1.1", "h2"}
			for _, v := range protos {
				l := ln.Mux.GetListener(v)
				if l == nil {
					return fmt.Errorf("could not get tls proto %q listener", v)
				}
				servers = append(servers, func() {
					go server.Serve(l)
				})
			}
		}

		return nil
	}

	configureForCluster := func(ln *base.ServerListener) error {
		// Clear out in case this is a second start of the controller
		ln.Mux.UnregisterProto(alpnmux.DefaultProto)
		l, err := ln.Mux.RegisterProto(alpnmux.DefaultProto, &tls.Config{
			GetConfigForClient: c.validateWorkerTls,
		})
		if err != nil {
			return fmt.Errorf("error getting sub-listener for worker proto: %w", err)
		}

		workerServer := grpc.NewServer(
			grpc.MaxRecvMsgSize(math.MaxInt32),
			grpc.MaxSendMsgSize(math.MaxInt32),
		)
		workerService := workers.NewWorkerServiceServer(c.ServersRepoFn, c.SessionRepoFn, c.workerStatusUpdateTimes, c.kms)
		pbs.RegisterServerCoordinationServiceServer(workerServer, workerService)
		pbs.RegisterSessionServiceServer(workerServer, workerService)

		interceptor := newInterceptingListener(c, l)
		ln.ALPNListener = interceptor
		ln.GrpcServer = workerServer

		servers = append(servers, func() {
			go workerServer.Serve(interceptor)
		})
		return nil
	}

	c.gatewayListener, _ = bufconnListener()
	servers = append(servers, func() {
		go c.gatewayServer.Serve(c.gatewayListener)
	})

	for _, ln := range c.conf.Listeners {
		var err error
		for _, purpose := range ln.Config.Purpose {
			switch purpose {
			case "api":
				err = configureForAPI(ln)
			case "cluster":
				err = configureForCluster(ln)
			case "proxy":
				// Do nothing, in a dev mode we might see it here
			default:
				err = fmt.Errorf("unknown listener purpose %q", purpose)
			}
			if err != nil {
				return err
			}
		}
	}

	for _, s := range servers {
		s()
	}

	return nil
}

func (c *Controller) stopListeners(serversOnly bool) error {
	serverWg := new(sync.WaitGroup)
	for _, ln := range c.conf.Listeners {
		localLn := ln
		serverWg.Add(1)
		go func() {
			defer serverWg.Done()

			shutdownKill, shutdownKillCancel := context.WithTimeout(c.baseContext, localLn.Config.MaxRequestDuration)
			defer shutdownKillCancel()

			if localLn.GrpcServer != nil {
				// Deal with the worst case
				go func() {
					<-shutdownKill.Done()
					localLn.GrpcServer.Stop()
				}()
				localLn.GrpcServer.GracefulStop()
			}
			if localLn.HTTPServer != nil {
				localLn.HTTPServer.Shutdown(shutdownKill)
			}
		}()
	}

	if c.gatewayServer != nil {
		serverWg.Add(1)
		go func() {
			defer serverWg.Done()
			shutdownKill, shutdownKillCancel := context.WithTimeout(c.baseContext, globals.DefaultMaxRequestDuration)
			defer shutdownKillCancel()
			go func() {
				<-shutdownKill.Done()
				c.gatewayServer.Stop()
			}()
			c.gatewayServer.GracefulStop()
		}()
	}

	serverWg.Wait()
	if serversOnly {
		return nil
	}
	var retErr *multierror.Error
	for _, ln := range c.conf.Listeners {
		if err := ln.Mux.Close(); err != nil {
			if _, ok := err.(*os.PathError); ok && ln.Config.Type == "unix" {
				// The rmListener probably tried to remove the file but it
				// didn't exist, ignore the error; this is a conflict
				// between rmListener and the default Go behavior of
				// removing auto-vivified Unix domain sockets.
			} else {
				retErr = multierror.Append(retErr, err)
			}
		}
	}
	return retErr.ErrorOrNil()
}

// bufconnListener will create an in-memory listener
func bufconnListener() (gatewayListener, string) {
	buffer := 1024 * 1024 // seems like a reasonable size for the ring buffer... happy to discuss
	return bufconn.Listen(buffer), ""
}

const gatewayTarget = ""

type gatewayListener interface {
	net.Listener
	Dial() (net.Conn, error)
}

func (c *Controller) gatewayDialOptions() []grpc.DialOption {
	return []grpc.DialOption{
		grpc.WithInsecure(),
		// grpc.WithBlock(),
		grpc.WithContextDialer(func(context.Context, string) (net.Conn, error) {
			return c.gatewayListener.Dial()
		}),
	}
}
