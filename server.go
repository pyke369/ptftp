package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/pyke369/golang-support/uconfig"
	"github.com/pyke369/golang-support/ulog"
)

var (
	config *uconfig.UConfig
	logger *ulog.ULog
	debug  bool
)

func server() {
	var err error

	cpath := fmt.Sprintf("/etc/%s.conf", progname)
	if len(os.Args) > 2 {
		cpath = os.Args[2]
	}
	if config, err = uconfig.New(cpath); err != nil {
		fmt.Fprintf(os.Stderr, "invalid configuration (%v) - exiting\n", err)
		os.Exit(1)
	}
	logger = ulog.New(config.GetString("server.log", "console(output=stdout)"))
	logger.Info(map[string]interface{}{"event": "start", "config": cpath, "pid": os.Getpid(), "version": version})

	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGHUP)
		for {
			signal := <-signals
			switch {
			case signal == syscall.SIGHUP:
				config.Load(cpath)
				logger.Load(config.GetString("server.log", "console(output=stdout)"))
				logger.Info(map[string]interface{}{"event": "reload", "config": cpath, "pid": os.Getpid(), "version": version})
			}
		}
	}()

	// start TFTP and HTTP listeners
	http.HandleFunc("/", httpHandler)
	for _, path := range config.GetPaths("server.listen") {
		if listen := strings.Split(config.GetStringMatch(path, "_", "^(tftp|http)@.*?:\\d+$"), "@"); listen[0] != "_" {
			if listen[0] == "tftp" {
				if address, err := net.ResolveUDPAddr("udp", strings.TrimLeft(listen[1], "*")); err == nil {
					logger.Info(map[string]interface{}{"event": "listen", "protocol": "tftp", "listen": listen[1]})
					go func(address *net.UDPAddr) {
						for {
							if handle, err := net.ListenUDP("udp", address); err == nil {
								packet := make([]byte, 512)
								for {
									packet = packet[:cap(packet)]
									if size, remote, err := handle.ReadFromUDP(packet); err == nil && size > 2 {
										go tftpHandler(packet[:size], handle.LocalAddr().String(), remote.String())
									}
								}
								handle.Close()
							}
							time.Sleep(time.Second)
						}
					}(address)
				}
			} else {
				server := &http.Server{
					Addr:        strings.TrimLeft(listen[1], "*"),
					ReadTimeout: time.Duration(config.GetDurationBounds("master.read_timeout", 10, 5, 60)) * time.Second,
					IdleTimeout: time.Duration(config.GetDurationBounds("master.idle_timeout", 20, 5, 60)) * time.Second,
				}
				logger.Info(map[string]interface{}{"event": "listen", "protocol": "http", "listen": listen[1]})
				go func(server *http.Server) {
					for {
						server.ListenAndServe()
						time.Sleep(time.Second)
					}
				}(server)
			}
		}
	}

	// activate/deactive debug logging
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGUSR1)
		for {
			<-signals
			debug = !debug
			if debug {
				logger.SetLevel("debug")
				logger.Debug(map[string]interface{}{"event": "debug", "state": "enabled"})
			} else {
				logger.Debug(map[string]interface{}{"event": "debug", "state": "disabled"})
				logger.SetLevel("info")
			}
		}
	}()

	// wait forever
	select {}
}
