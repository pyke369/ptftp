package tftp

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pyke369/golang-support/rcache"
	"github.com/pyke369/golang-support/uconfig"
	"github.com/pyke369/golang-support/ulog"
	"github.com/pyke369/golang-support/ustr"

	b "ptftp/backend"
	c "ptftp/cache"
)

func Handle(config *uconfig.UConfig, logger *ulog.ULog, packet []byte, local, remote string) {
	alocal, _ := net.ResolveUDPAddr("udp", local)
	aremote, _ := net.ResolveUDPAddr("udp", remote)
	if alocal != nil && aremote != nil {
		alocal.Port = 0
		if handle, err := net.DialUDP("udp", alocal, aremote); err == nil {
			defer func() {
				handle.Close()
			}()

			// check packet type and bail out if not a proper request
			if opcode := binary.BigEndian.Uint16(packet); opcode != 1 {
				handle.Write(append([]byte{0, 5, 0, 4}, append([]byte("read requests only"), 0)...))
				return
			}

			// parse packet (file, mode and options)
			file, option, options, blksize, timeout, tsize, wsize := "", "", map[string]string{}, 512, 5, int64(-1), 1
			mode, target, ftarget, content, headers, env := "", "", "", []byte{}, map[string]string{}, []string{}
			for index, field := range bytes.Split(packet[2:], []byte{0}) {
				switch index {
				case 0:
					file = string(bytes.TrimSpace(bytes.ReplaceAll(bytes.ReplaceAll(bytes.ReplaceAll(bytes.ReplaceAll(field,
						[]byte("../"), []byte{}), []byte("./"), []byte{}), []byte("&"), []byte{}), []byte(";"), []byte{})))

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
				soptions += name + "=" + value
			}
			logger.Info(map[string]any{"scope": "tftp", "event": "request", "local": handle.LocalAddr().String(), "remote": remote, "file": file,
				"options": strings.TrimSpace(soptions)})

			// check routes/backends and gather requested file information
			if file == "" {
				handle.Write(append([]byte{0, 5, 0, 1}, append([]byte("file not found"), 0)...))
				return
			}
			for _, path := range config.Paths("routes") {
				if route := config.String(path); route != "" {
					if match := config.String(config.Path("routes", route, "match")); match != "" {
						if matcher := rcache.Get(match); matcher != nil && matcher.MatchString(file) {
							for _, path := range config.Paths(config.Path("routes", route, "backends")) {
								backend := config.String(path)
								mode = strings.ToLower(config.String(config.Path("routes", route, backend, "mode")))
								target = matcher.ReplaceAllString(file, config.String(config.Path("routes", route, backend, "target")))
								switch mode {
								case "file":
									ftarget = target
									tsize, content, _ = b.File(target, 0, 64<<10)

								case "http":
									for _, path := range config.Paths(config.Path("routes", route, backend, "headers")) {
										if value := strings.TrimSpace(config.String(path)); value != "" {
											if parts := strings.Split(value, ":"); len(parts) > 1 {
												headers[parts[0]] = matcher.ReplaceAllString(file, strings.TrimSpace(strings.Join(parts[1:], ":")))
											}
										}
									}
									tsize, content, _ = b.HTTP(target, 0, 64<<10, timeout, headers)
									if tsize >= 0 {
										for _, policy := range config.Strings(config.Path("routes", route, backend, "cache", "policies")) {
											prefix := config.Path("routes", route, backend, "cache", policy)
											if match = config.String(config.Path(prefix, "match")); match != "" {
												if cmatcher := rcache.Get(match); cmatcher != nil && cmatcher.MatchString(file) {
													if path := matcher.ReplaceAllString(file, config.String(config.Path(prefix, "path"))); path != "" {
														c.Queue(&c.Job{
															Trigger:     "tftp",
															Remote:      target,
															Local:       path,
															Headers:     headers,
															Delay:       config.DurationBounds(config.Path(prefix, "delay"), 5, 1, 60),
															Concurrency: int(config.IntegerBounds(config.Path(prefix, "concurrency"), 8, 1, 16)),
															Refresh:     int(config.DurationBounds(config.Path(prefix, "refresh"), 0, 0, 30*86400)),
														})
														break
													}
												}
											}
										}
									}

								case "exec":
									for _, path := range config.Paths(config.Path("routes", route, backend, "env")) {
										if value := strings.TrimSpace(config.String(path)); value != "" {
											if parts := strings.Split(value, ":"); len(parts) > 1 {
												env = append(env, parts[0]+"="+matcher.ReplaceAllString(file, strings.TrimSpace(strings.Join(parts[1:], ":"))))
											}
										}
									}
									tsize, content, _ = b.Exec(target, timeout, env)
								}
								if tsize >= 0 {
									break
								}
							}
							break
						}
					}
				}
			}
			if tsize < 0 {
				handle.Write(append([]byte{0, 5, 0, 1}, append([]byte("file not found"), 0)...))
				logger.Warn(map[string]any{"scope": "tftp", "event": "error", "local": handle.LocalAddr().String(), "remote": remote, "file": file,
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
						value = strconv.FormatInt(tsize, 10)
					}
					if name == "windowsize" {
						if wsize, _ = strconv.Atoi(value); wsize < 1 || wsize > 65535 {
							wsize = 1
							continue
						}
					}
					lpacket = append(lpacket, []byte(name)...)
					lpacket = append(lpacket, 0)
					lpacket = append(lpacket, []byte(value)...)
					lpacket = append(lpacket, 0)
					soptions += name + "=" + value
				}
				if len(lpacket) > 2 {
					handle.Write(lpacket)
				}
			}

			// send requested content
			toffset, coffset, block, retries := int64(0), int64(0), uint16(0), 0
			sstart := time.Now()
		sloop:
			for {
				if retries > 2 {
					logger.Warn(map[string]any{"scope": "tftp", "event": "error", "local": handle.LocalAddr().String(), "remote": remote, "file": file,
						"mode": mode, "size": tsize, "sent": toffset, "code": 0, "message": "retries count exceeded"})
					break sloop
				}
				bsize := min(int64(blksize), tsize-toffset)
				if bsize > 0 && coffset+bsize > int64(len(content)) {
					if mode == "http" && ftarget != "" {
						if info, err := os.Stat(ftarget); err == nil && info.Mode().IsRegular() && info.Size() == tsize {
							mode, target = "file", ftarget
						}
					}
					blocks := config.SizeBounds("block_size", 4<<20, 1<<20, 16<<20) / int64(blksize)
					switch mode {
					case "file":
						_, content, _ = b.File(target, toffset, int64(blksize)*blocks)

					case "http":
						_, content, _ = b.HTTP(target, toffset, int64(blksize)*blocks, timeout, headers)
					}
					coffset = 0
				}
				lpacket = lpacket[:bsize+4]
				binary.BigEndian.PutUint16(lpacket[0:], 3)
				block = uint16(((toffset / int64(blksize)) + 1))
				binary.BigEndian.PutUint16(lpacket[2:], block)
				if bsize > 0 && int64(len(content)) >= coffset+bsize {
					copy(lpacket[4:], content[coffset:coffset+bsize])
				}
				handle.Write(lpacket)
				retries++
				rstart := time.Now()
			aloop:
				for {
					lpacket = lpacket[:cap(lpacket)]
					elapsed := int(time.Since(rstart) / time.Second)
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
								if binary.BigEndian.Uint16(lpacket[2:]) == block {
									toffset += bsize
									coffset += bsize
									retries = 0
									if bsize < int64(blksize) {
										duration := time.Since(sstart)
										logger.Info(map[string]any{"scope": "tftp", "event": "response", "local": handle.LocalAddr().String(), "remote": remote,
											"file": file, "mode": mode, "size": tsize, "sent": toffset, "duration": ustr.Duration(duration),
											"bandwidth": ustr.Bandwidth((toffset * 8) / int64(duration) / int64(time.Second))})
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
							logger.Warn(map[string]any{"scope": "tftp", "event": "error", "local": handle.LocalAddr().String(), "remote": remote,
								"file": file, "mode": mode, "size": tsize, "sent": toffset, "code": code, "message": message})
							break sloop

						default:
							handle.Write(append([]byte{0, 5, 0, 4}, append([]byte("illegal TFTP operation"), 0)...))
							logger.Warn(map[string]any{"scope": "tftp", "event": "error", "local": handle.LocalAddr().String(), "remote": remote,
								"file": file, "mode": mode, "size": tsize, "sent": toffset, "code": 4, "message": "illegal TFTP operation" + strconv.Itoa(int(opcode))})
							break sloop
						}
					}
				}
			}
		}
	}
}
