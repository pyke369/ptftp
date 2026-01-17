package server

import (
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/pyke369/golang-support/uconfig"
	"github.com/pyke369/golang-support/ulog"

	c "ptftp/cache"
	"ptftp/common"
	h "ptftp/http"
	t "ptftp/tftp"
)

func Run() {
	path := filepath.Join("/"+"etc", common.PROGNAME+".conf")
	if len(os.Args) > 2 {
		path = os.Args[2]
	}
	config, err := uconfig.New(path)
	if err != nil {
		os.Stderr.WriteString(err.Error() + " - exiting\n")
		os.Exit(1)
	}
	config.SetPrefix(common.PROGNAME)
	logger := ulog.New(config.String("log", "console()"))
	logger.SetOrder([]string{"scope", "event", "version", "config", "pid", "listen", "trigger", "remote", "local", "size", "duration", "bandwidth"})
	logger.Info(map[string]any{"scope": "server", "event": "start", "version": common.PROGVER, "config": path, "pid": os.Getpid()})

	c.Run(config, logger)

	for _, listen := range config.Strings("listen") {
		parts := strings.Split(listen, "@")
		if len(parts) != 2 {
			continue
		}
		parts[0] = strings.ToLower(strings.TrimSpace(parts[0]))
		parts[1] = strings.TrimSpace(parts[1])

		switch parts[0] {
		case "tftp":
			if address, err := net.ResolveUDPAddr("udp", strings.TrimLeft(parts[1], "*")); err == nil {
				logger.Info(map[string]any{"scope": "server", "event": "listen", "listen": "tftp@" + parts[1]})
				go func(address *net.UDPAddr) {
					for {
						if handle, err := net.ListenUDP("udp", address); err == nil {
							packet := make([]byte, 512)
							for {
								packet = packet[:cap(packet)]
								if size, remote, err := handle.ReadFromUDP(packet); err == nil && size > 2 {
									go t.Handle(config, logger, packet[:size], handle.LocalAddr().String(), remote.String())
								}
							}
						}
						time.Sleep(time.Second)
					}
				}(address)
			}

		case "http":
			if _, err := net.ResolveTCPAddr("tcp", strings.TrimLeft(parts[1], "*")); err == nil {
				server := &http.Server{
					Handler:     h.Handler(config, logger),
					Addr:        strings.TrimLeft(parts[1], "*"),
					ReadTimeout: config.DurationBounds("read_timeout", 10, 5, 60),
					IdleTimeout: config.DurationBounds("idle_timeout", 15, 5, 60),
				}
				logger.Info(map[string]any{"scope": "server", "event": "listen", "listen": "http@" + parts[1]})
				go func(server *http.Server) {
					for {
						server.ListenAndServe()
						time.Sleep(time.Second)
					}
				}(server)
			}
		}
	}

	select {}
}
