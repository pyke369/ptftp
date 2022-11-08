package main

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"path"
	"strconv"
	"time"
)

func ClientBandwidth(bandwidth float64) string {
	if bandwidth < (1000*1000)/8 {
		return fmt.Sprintf("%.0fkb/s", (bandwidth*8)/1000)
	}
	return fmt.Sprintf("%.2fMb/s", (bandwidth*8)/(1000*1000))
}

func ClientDuration(duration time.Duration) string {
	if duration < time.Millisecond {
		return fmt.Sprintf("%.0fus", float64(duration)/float64(time.Microsecond))
	}
	if duration < time.Second {
		return fmt.Sprintf("%.0fms", float64(duration)/float64(time.Millisecond))
	}
	return fmt.Sprintf("%.2fs", float64(duration)/float64(time.Second))
}

func clientSize(size int) string {
	if size < 1024 {
		return fmt.Sprintf("%dB", size)
	} else if size < 1024*1024 {
		return fmt.Sprintf("%dkB", size/1024)
	} else if size < 10*1024*1024 {
		return fmt.Sprintf("%.2fMB", float64(size)/(1024*1024))
	} else if size < 100*1024*1024 {
		return fmt.Sprintf("%.1fMB", float64(size)/(1024*1024))
	} else if size < 1024*1024*1024 {
		return fmt.Sprintf("%dMB", size/(1024*1024))
	}
	return fmt.Sprintf("%.2fGB", float64(size)/(1024*1024*1024))
}

func Client() {
	remote, rfile, lfile, target, received, start := os.Args[1], os.Args[2], "", os.Stdout, 0, time.Time{}
	if _, _, err := net.SplitHostPort(remote); err != nil {
		remote += ":69"
	}
	if aremote, err := net.ResolveUDPAddr("udp", remote); err != nil {
		fmt.Fprintf(os.Stderr, "%v - aborting\n", err)
		os.Exit(2)
	} else {
		if len(os.Args) >= 4 {
			lfile = os.Args[3]
		}
		if lfile == "" {
			lfile = path.Base(rfile)
		}
		if lfile != "-" {
			if value, err := os.OpenFile(lfile, os.O_CREATE|os.O_TRUNC|os.O_RDWR, 0644); err == nil {
				target = value
			} else {
				fmt.Fprintf(os.Stderr, "%v - aborting\n", err)
				os.Exit(2)
			}
		}
		if handle, err := net.ListenUDP("udp", nil); err == nil {
			packet, retries, timeout, receiving, tsize, blksize, block, lsize := make([]byte, 128<<10), 0, 4, false, 0, 512, uint16(0), -1
			for {
				if retries > 2 {
					fmt.Fprintf(os.Stderr, "retries count exceeded - aborting\n")
					os.Exit(3)
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
				if _, err := handle.WriteToUDP(packet, aremote); err != nil {
					fmt.Fprintf(os.Stderr, "\r%v - aborting\n", err)
					os.Exit(3)
				}
			rloop:
				for {
					if retries > 2 {
						fmt.Fprintf(os.Stderr, "\rretries count exceeded - aborting\n")
						os.Exit(3)
					}
					packet = packet[:cap(packet)]
					handle.SetReadDeadline(time.Now().Add(time.Duration(timeout) * time.Second))
					if size, nremote, err := handle.ReadFromUDP(packet); err == nil && size > 2 {
						aremote = nremote
						packet = packet[:size]
						opcode := binary.BigEndian.Uint16(packet)
						switch opcode {
						case 3:
							if start.IsZero() {
								start = time.Now()
							}
							receiving, retries = true, 0
							if size >= 4 {
								block = binary.BigEndian.Uint16(packet[2:])
								if lsize = len(packet) - 4; lsize > 0 {
									if written, err := target.Write(packet[4:]); err != nil || written != lsize {
										fmt.Printf("\r%v - aborting\n", err)
										os.Exit(3)
									}
								}
								received += lsize
								if duration := float64(time.Since(start)) / float64(time.Second); duration > 0 {
									if tsize != 0 {
										fmt.Fprintf(os.Stderr, "\r%s/%s (%s)  ", clientSize(received), clientSize(tsize), ClientBandwidth(float64(received)/duration))
									} else {
										fmt.Fprintf(os.Stderr, "\r%s (%s)  ", clientSize(received), ClientBandwidth(float64(received)/duration))
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
								fmt.Fprintf(os.Stderr, "\r%s", message)
							case 1:
								fmt.Fprintf(os.Stderr, "\rfile not found")
							case 2:
								fmt.Fprintf(os.Stderr, "\raccess violation")
							case 3:
								fmt.Fprintf(os.Stderr, "\rdisk full or allocation exceeded")
							case 4:
								fmt.Fprintf(os.Stderr, "\rillegal TFTP operation")
							case 5:
								fmt.Fprintf(os.Stderr, "\runknown transfer id")
							case 6:
								fmt.Fprintf(os.Stderr, "\rfile already exists")
							case 7:
								fmt.Fprintf(os.Stderr, "\rno such user")
							}
							fmt.Fprintf(os.Stderr, " - aborting\n")
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
												tsize = value
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
						break rloop
					}

					// send ACK packets
					packet = packet[:4]
					binary.BigEndian.PutUint16(packet[0:], 4)
					binary.BigEndian.PutUint16(packet[2:], block)
					handle.WriteToUDP(packet, aremote)
					if lsize >= 0 && lsize < blksize {
						if duration := float64(time.Since(start)) / float64(time.Second); duration > 0 {
							fmt.Fprintf(os.Stderr, "\r%s in %s (%s)               \n", clientSize(received), ClientDuration(time.Since(start)), ClientBandwidth(float64(received)/duration))
						}
						target.Close()
						os.Exit(0)
					}
					retries++
				}
				retries++
			}
		} else {
			fmt.Fprintf(os.Stderr, "\r%v - aborting\n", err)
			os.Exit(2)
		}
	}
}
