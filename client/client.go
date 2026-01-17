package client

import (
	"bytes"
	"encoding/binary"
	"net"
	"os"
	"path"
	"strconv"
	"time"

	"github.com/pyke369/golang-support/ustr"
)

func bail(message string, exit int) {
	os.Stderr.WriteString("\r" + message + " - aborting\n")
	os.Exit(exit)
}

func Run() {
	if _, _, err := net.SplitHostPort(os.Args[1]); err != nil {
		os.Args[1] += ":69"
	}
	remote, err := net.ResolveUDPAddr("udp", os.Args[1])
	if err != nil {
		bail(err.Error(), 2)
	}

	rfile, lfile, target := os.Args[2], "", os.Stdout
	if len(os.Args) >= 4 {
		lfile = os.Args[3]
	}
	if lfile == "" {
		lfile = path.Base(rfile)
	}
	if lfile != "-" {
		value, err := os.OpenFile(lfile, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0o644)
		if err != nil {
			bail(err.Error(), 2)
		}
		target = value
	}

	handle, err := net.ListenUDP("udp", nil)
	if err != nil {
		bail(err.Error(), 2)
	}

	start, received, packet, retries, timeout, receiving, tsize, blksize, block, lsize := time.Now(), int64(0), make([]byte, 128<<10), 0, 5, false, int64(0), 512, uint16(0), -1
	for {
		if retries > 2 {
			bail("retries count exceeded", 3)
		}

		// send RRQ packet
		packet = packet[:0]
		packet = append(packet, []byte{0, 1}...)
		packet = append(packet, append([]byte(rfile), 0)...)
		packet = append(packet, append([]byte("octet"), 0)...)
		packet = append(packet, append([]byte("blksize"), 0)...)
		packet = append(packet, append([]byte("16384"), 0)...)
		packet = append(packet, append([]byte("tsize"), 0)...)
		packet = append(packet, append([]byte("0"), 0)...)
		if _, err := handle.WriteToUDP(packet, remote); err != nil {
			os.Stderr.WriteString("\r" + err.Error() + " - aborting\n")
			os.Exit(3)
		}

		for {
			if retries > 2 {
				os.Stderr.WriteString("\rretries count exceeded - aborting\n")
				os.Exit(3)
			}
			packet = packet[:cap(packet)]
			handle.SetReadDeadline(time.Now().Add(time.Duration(timeout) * time.Second))
			if size, nremote, err := handle.ReadFromUDP(packet); err == nil && size > 2 {
				remote, packet = nremote, packet[:size]
				switch binary.BigEndian.Uint16(packet) {
				case 3:
					if start.IsZero() {
						start = time.Now()
					}
					receiving, retries = true, 0
					if size >= 4 {
						block = binary.BigEndian.Uint16(packet[2:])
						if lsize = len(packet) - 4; lsize > 0 {
							if written, err := target.Write(packet[4:]); err != nil || written != lsize {
								bail(err.Error(), 3)
							}
						}
						received += int64(lsize)
						if duration := time.Since(start) / time.Second; duration > 0 {
							if tsize != 0 {
								os.Stderr.WriteString("\r" + ustr.Size(received) + "/" + ustr.Size(tsize) + " (" + ustr.Bandwidth(received/int64(duration)) + ")  ")

							} else {
								os.Stderr.WriteString("\r" + ustr.Size(received) + " (" + ustr.Bandwidth(received/int64(duration)) + ")  ")
							}
						}
					}

				case 5:
					code, message := 0, ""
					if size >= 4 {
						code = int(binary.BigEndian.Uint16(packet[2:]))
						if size >= 6 {
							message = string(packet[4 : len(packet)-1])
						}
					}
					switch code {
					case 0:
						bail(message, 3)

					case 1:
						bail("file not found", 3)

					case 2:
						bail("access violation", 3)

					case 3:
						bail("disk full or allocation exceeded", 3)

					case 4:
						bail("illegal TFTP operation", 3)

					case 5:
						bail("unknown transfer id", 3)

					case 6:
						bail("file already exists", 3)

					case 7:
						bail("no such user", 3)
					}
					os.Exit(3)

				case 6:
					receiving, retries = true, 0
					if size >= 3 {
						option := ""
						for index, field := range bytes.Split(packet[2:], []byte{0}) {
							if index%2 == 0 && len(field) > 0 {
								option = string(bytes.ToLower(field))

							} else if option != "" {
								switch option {
								case "tsize":
									if value, err := strconv.Atoi(string(field)); err == nil && value >= 0 {
										tsize = int64(value)
									}

								case "blksize":
									if value, err := strconv.Atoi(string(field)); err == nil && value >= 8 && value <= 65464 {
										blksize = value
									}
								}
								option = ""
							}
						}
					}
				}

			} else if !receiving {
				// retry RRQ
				break
			}

			// send ACK packets
			packet = packet[:4]
			binary.BigEndian.PutUint16(packet[0:], 4)
			binary.BigEndian.PutUint16(packet[2:], block)
			handle.WriteToUDP(packet, remote)
			if lsize >= 0 && lsize < blksize {
				if duration := time.Since(start) / time.Second; duration > 0 {
					os.Stderr.WriteString("\r" + ustr.Size(received) + " in " + ustr.Duration(time.Since(start)) + " (" + ustr.Bandwidth(received/int64(duration)) + ")               \n")
				}
				target.Close()
				os.Exit(0)
			}
			retries++
		}
		retries++
	}
}
