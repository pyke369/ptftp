package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"math"
	"net"
	"strconv"
	"strings"
	"time"

	"github.com/pyke369/golang-support/rcache"
	"github.com/pyke369/golang-support/uconfig"
)

func tftpHandler(packet []byte, local, remote string) {
	alocal, _ := net.ResolveUDPAddr("udp", local)
	aremote, _ := net.ResolveUDPAddr("udp", remote)
	if alocal != nil && aremote != nil {
		alocal.Port = 0
		if handle, err := net.DialUDP("udp", alocal, aremote); err == nil {
			defer func() { handle.Close() }()

			// check packet type and bail out if not a request
			if opcode := binary.BigEndian.Uint16(packet); opcode != 1 {
				handle.Write(append([]byte{0, 5, 0, 4}, append([]byte("read requests only"), 0)...))
				return
			}

			// parse packet (file, mode and options)
			file, option, options, blksize, timeout, tsize, windowsize := "", "", map[string]string{}, 512, 4, -1, 1
			mode, target, content, headers, env := "", "", []byte{}, map[string]string{}, []string{}
			_ = windowsize
			for index, field := range bytes.Split(packet[2:], []byte{0}) {
				switch index {
				case 0:
					file = string(bytes.TrimSpace(bytes.Replace(bytes.Replace(bytes.Replace(bytes.Replace(field,
						[]byte("../"), []byte{}, -1), []byte("./"), []byte{}, -1), []byte("&"), []byte{}, -1), []byte(";"), []byte{}, -1)))
				case 1:
					if value := string(bytes.ToLower(field)); value != "netascii" && value != "octet" && value != "mail" {
						handle.Write(append([]byte{0, 5, 0, 4}, append([]byte("unknown transfer mode"), 0)...))
						return
					}
				default:
					if index%2 == 0 && len(field) > 0 {
						option = string(bytes.ToLower(field))
						if option != "blksize" && option != "timeout" && option != "tsize" && option != "windowsize" {
							option = ""
						}
					} else if option != "" {
						options[option] = string(field)
						option = ""
					}
				}
			}
			soptions := ""
			for name, value := range options {
				soptions += fmt.Sprintf("%s=%s ", name, value)
			}
			log.Info(map[string]interface{}{"scope": "tftp", "event": "request", "local": handle.LocalAddr().String(), "remote": remote, "file": file,
				"options": strings.TrimSpace(soptions)})

			// check routes/backends and gather requested file information
			if file == "" {
				handle.Write(append([]byte{0, 5, 0, 1}, append([]byte("file not found"), 0)...))
				return
			}
			for _, path := range config.GetPaths("server.routes") {
				if route := config.GetString(path, ""); route != "" {
					if match := config.GetString("routes."+route+".match", ""); match != "" {
						if matcher := rcache.Get(match); matcher != nil && matcher.MatchString(file) {
						found:
							for _, path := range config.GetPaths("routes." + route + ".backends") {
								backend := config.GetString(path, "")
								mode = strings.ToLower(config.GetString("routes."+route+"."+backend+".mode", ""))
								target = matcher.ReplaceAllString(file, config.GetString("routes."+route+"."+backend+".target", ""))
								switch mode {
								case "file":
									tsize, content = backendFile(target, 0, 64<<10)
								case "http":
									for _, path := range config.GetPaths("routes." + route + "." + backend + ".headers") {
										if value := strings.TrimSpace(config.GetString(path, "")); value != "" {
											if parts := strings.Split(value, ":"); len(parts) > 1 {
												headers[parts[0]] = matcher.ReplaceAllString(file, strings.TrimSpace(strings.Join(parts[1:], ":")))
											}
										}
									}
									tsize, content = backendHTTP(target, 0, 64<<10, timeout, headers)
									if match = config.GetString("routes."+route+"."+backend+".cache.match", ""); match != "" && tsize >= 0 {
										if cmatcher := rcache.Get(match); cmatcher != nil && cmatcher.MatchString(file) {
											if path := matcher.ReplaceAllString(file, config.GetString("routes."+route+"."+backend+".cache.path", "")); path != "" {
												select {
												case cacheJobs <- CACHEJOB{"tftp", target, path, tsize, headers,
													uconfig.Duration(config.GetDurationBounds("routes."+route+"."+backend+".cache.delay", 3, 1, 60)),
													int(config.GetIntegerBounds("routes."+route+"."+backend+".cache.concurrency", 32, 1, 32)),
												}:
												default:
												}
											}
										}
									}
								case "exec":
									for _, path := range config.GetPaths("routes." + route + "." + backend + ".env") {
										if value := strings.TrimSpace(config.GetString(path, "")); value != "" {
											if parts := strings.Split(value, ":"); len(parts) > 1 {
												env = append(env, fmt.Sprintf("%s=%s", parts[0], matcher.ReplaceAllString(file, strings.TrimSpace(strings.Join(parts[1:], ":")))))
											}
										}
									}
									tsize, content = backendExec(target, timeout, env)
								}
								if tsize >= 0 {
									break found
								}
							}
							break
						}
					}
				}
			}
			if tsize < 0 {
				handle.Write(append([]byte{0, 5, 0, 1}, append([]byte("file not found"), 0)...))
				log.Warn(map[string]interface{}{"scope": "tftp", "event": "error", "local": handle.LocalAddr().String(), "remote": remote, "file": file,
					"code": 1, "message": "file not found"})
				return
			}

			// send options acknowledgment packet
			lpacket := make([]byte, 128<<10)
			lpacket = lpacket[:0]
			if len(options) > 0 {
				lpacket = append(lpacket, []byte{0x00, 0x06}...)
				soptions := ""
				for name, value := range options {
					if name == "blksize" {
						if blksize, _ = strconv.Atoi(value); blksize < 8 || blksize > 65464 {
							blksize = 512
							continue
						}
					}
					if name == "timeout" {
						if timeout, _ = strconv.Atoi(value); timeout < 1 || timeout > 255 {
							timeout = 5
							continue
						}
					}
					if name == "tsize" {
						value = fmt.Sprintf("%d", tsize)
					}
					if name == "windowsize" {
						if windowsize, _ = strconv.Atoi(value); windowsize < 1 || windowsize > 65535 {
							timeout = 5
							continue
						}
					}
					lpacket = append(lpacket, []byte(name)...)
					lpacket = append(lpacket, 0)
					lpacket = append(lpacket, []byte(value)...)
					lpacket = append(lpacket, 0)
					soptions += fmt.Sprintf("%s=%s ", name, value)
				}
				if len(lpacket) > 2 {
					log.Debug("%s > %s OACK (%s)", handle.LocalAddr().String(), remote, strings.TrimSpace(soptions))
					handle.Write(lpacket)
				}
			}

			// send requested content
			toffset, coffset, block, retries := 0, 0, 0, 0
			sstart := time.Now()
		sloop:
			for {
				if retries > 2 {
					log.Warn(map[string]interface{}{"scope": "tftp", "event": "error", "local": handle.LocalAddr().String(), "remote": remote, "file": file,
						"mode": mode, "size": tsize, "sent": toffset, "code": 0, "message": "retries count exceeded"})
					break sloop
				}
				bsize := int(math.Min(float64(blksize), float64(tsize-toffset)))
				if bsize > 0 && coffset+bsize > len(content) {
					count := int(config.GetSizeBounds("server.block_size", 2<<20, 1<<20, 16<<20)) / blksize
					switch mode {
					case "file":
						_, content = backendFile(target, toffset, blksize*count)
					case "http":
						_, content = backendHTTP(target, toffset, blksize*count, timeout, headers)
					}
					coffset = 0
				}
				lpacket = lpacket[:bsize+4]
				binary.BigEndian.PutUint16(lpacket[0:], 3)
				block = ((toffset / blksize) + 1) % 65536
				binary.BigEndian.PutUint16(lpacket[2:], uint16(block))
				if bsize > 0 && len(content) >= coffset+bsize {
					copy(lpacket[4:], content[coffset:coffset+bsize])
				}
				handle.Write(lpacket)
				log.Debug("%s > %s DATA (block %d / %d bytes)", handle.LocalAddr().String(), remote, block, bsize)
				retries++
				rstart := time.Now()
			aloop:
				for {
					lpacket = lpacket[:cap(lpacket)]
					elapsed := int(time.Now().Sub(rstart) / time.Second)
					if elapsed >= timeout {
						break aloop
					}
					handle.SetReadDeadline(time.Now().Add(time.Duration(timeout-elapsed) * time.Second))
					if size, _, err := handle.ReadFromUDP(lpacket); err == nil && size > 2 {
						lpacket = lpacket[:size]
						opcode := binary.BigEndian.Uint16(lpacket)
						switch opcode {
						case 4:
							if size >= 4 {
								ablock := binary.BigEndian.Uint16(lpacket[2:])
								log.Debug("%s > %s ACK (block %d)", remote, handle.LocalAddr().String(), ablock)
								if int(ablock) == block {
									toffset += bsize
									coffset += bsize
									retries = 0
									if bsize < blksize {
										duration := time.Now().Sub(sstart)
										log.Info(map[string]interface{}{"scope": "tftp", "event": "response", "local": handle.LocalAddr().String(), "remote": remote,
											"file": file, "mode": mode, "size": tsize, "sent": toffset, "duration": hduration(duration),
											"bandwidth": hbandwidth(float64(toffset) / (float64(duration) / float64(time.Second)))})
										break sloop
									}
									break aloop
								}
							}
						case 5:
							code := uint16(0)
							message := ""
							if size >= 4 {
								code = binary.BigEndian.Uint16(lpacket[2:])
							}
							if size >= 5 && lpacket[len(lpacket)-1] == 0 {
								message = string(lpacket[4 : len(lpacket)-1])
							}
							log.Warn(map[string]interface{}{"scope": "tftp", "event": "error", "local": handle.LocalAddr().String(), "remote": remote,
								"file": file, "mode": mode, "size": tsize, "sent": toffset, "code": code, "message": message})
							break sloop
						default:
							handle.Write(append([]byte{0, 5, 0, 4}, append([]byte("illegal TFTP operation"), 0)...))
							log.Warn(map[string]interface{}{"scope": "tftp", "event": "error", "local": handle.LocalAddr().String(), "remote": remote,
								"file": file, "mode": mode, "size": tsize, "sent": toffset, "code": 4, "message": fmt.Sprintf("illegal TFTP operation %d", opcode)})
							break sloop
						}
					}
				}
			}
		}
	}
}
