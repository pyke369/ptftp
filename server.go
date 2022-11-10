package main

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/pyke369/golang-support/uconfig"
	"github.com/pyke369/golang-support/ulog"
)

var (
	Config *uconfig.UConfig
	Logger *ulog.ULog
	debug  bool
)

func Server() {
	var err error

	cpath := filepath.Join("/etc", PROGNAME+".conf")
	if len(os.Args) > 2 {
		cpath = os.Args[2]
	}
	if Config, err = uconfig.New(cpath); err != nil {
		fmt.Fprintf(os.Stderr, "invalid configuration (%v) - exiting\n", err)
		os.Exit(1)
	}
	Logger = ulog.New(Config.GetString("server.log", "console(output=stdout,time=msdatetime)"))
	Logger.Info(map[string]interface{}{"scope": "server", "event": "start", "config": cpath, "pid": os.Getpid(), "version": VERSION})

	// handle configuration reload and activate/deactive debug logging
	go func() {
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGHUP, syscall.SIGUSR1)
		for {
			signal := <-signals
			switch signal {
			case syscall.SIGHUP:
				Config.Load(cpath)
				Logger.Load(Config.GetString("server.log", "console(output=stdout,time=msdatetime)"))
				Logger.Info(map[string]interface{}{"scope": "server", "event": "reload", "config": cpath, "pid": os.Getpid(), "version": VERSION})

			case syscall.SIGUSR1:
				debug = !debug
				if debug {
					Logger.SetLevel("debug")
					Logger.Debug(map[string]interface{}{"scope": "server", "event": "debug", "state": "enabled"})
				} else {
					Logger.Debug(map[string]interface{}{"scope": "server", "event": "debug", "state": "disabled"})
					Logger.SetLevel("info")
				}
			}
		}
	}()

	// start cache handler
	CacheHandler()

	// start TFTP and HTTP listeners
	http.HandleFunc("/", HttpHandler)
	for _, path := range Config.GetPaths("server.listen") {
		if listen := strings.Split(Config.GetStringMatch(path, "_", "^(tftp|http)@.*?:\\d+$"), "@"); listen[0] != "_" {
			if listen[0] == "tftp" {
				if address, err := net.ResolveUDPAddr("udp", strings.TrimLeft(listen[1], "*")); err == nil {
					Logger.Info(map[string]interface{}{"scope": "server", "event": "listen", "protocol": "tftp", "listen": listen[1]})
					go func(address *net.UDPAddr) {
						for {
							if handle, err := net.ListenUDP("udp", address); err == nil {
								packet := make([]byte, 512)
								for {
									packet = packet[:cap(packet)]
									if size, remote, err := handle.ReadFromUDP(packet); err == nil && size > 2 {
										go TftpHandler(packet[:size], handle.LocalAddr().String(), remote.String())
									}
								}
							}
							time.Sleep(time.Second)
						}
					}(address)
				}
			} else {
				server := &http.Server{
					Addr:        strings.TrimLeft(listen[1], "*"),
					ReadTimeout: uconfig.Duration(Config.GetDurationBounds("server.read_timeout", 10, 5, 60)),
					IdleTimeout: uconfig.Duration(Config.GetDurationBounds("server.idle_timeout", 20, 5, 60)),
				}
				Logger.Info(map[string]interface{}{"scope": "server", "event": "listen", "protocol": "http", "listen": listen[1]})
				go func(server *http.Server) {
					for {
						server.ListenAndServe()
						time.Sleep(time.Second)
					}
				}(server)
			}
		}
	}

	// wait forever
	select {}
}
