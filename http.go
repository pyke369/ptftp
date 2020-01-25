package main

import (
	"fmt"
	"math"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/pyke369/golang-support/rcache"
	"github.com/pyke369/golang-support/uconfig"
)

func httpHandler(response http.ResponseWriter, request *http.Request) {
	file := strings.Replace(strings.Replace(strings.Replace(strings.Replace(request.URL.Path, "../", "", -1), "./", "", -1), "&", "", -1), ";", "", -1)
	status, timeout, tsize, mode, target, ftarget, content, headers, env, start, end, sent := 200, 10, -1, "", "", "", []byte{}, map[string]string{}, []string{}, 0, -1, 0
	sstart := time.Now()
	log.Info(map[string]interface{}{"scope": "http", "event": "request", "file": file, "remote": request.RemoteAddr})
	defer func() {
		if status/100 > 2 {
			log.Warn(map[string]interface{}{"scope": "http", "event": "response", "remote": request.RemoteAddr, "file": file, "status": status})
		} else {
			duration := time.Now().Sub(sstart)
			log.Info(map[string]interface{}{"scope": "http", "event": "response", "remote": request.RemoteAddr, "file": file, "mode": mode,
				"status": status, "size": tsize, "sent": sent, "duration": hduration(duration),
				"bandwidth": hbandwidth(float64(sent) / (float64(duration) / float64(time.Second)))})
		}
	}()
	response.Header().Set("Server", progname+"/"+version)
	response.Header().Set("Accept-Ranges", "bytes")
	if request.Method != http.MethodHead && request.Method != http.MethodGet {
		status = http.StatusMethodNotAllowed
		response.WriteHeader(status)
		return
	}

	// check routes/backends and gather requested file information
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
							ftarget = target
							tsize, _ = backendFile(target, 0, 1)
						case "http":
							for _, path := range config.GetPaths("routes." + route + "." + backend + ".headers") {
								if value := strings.TrimSpace(config.GetString(path, "")); value != "" {
									if parts := strings.Split(value, ":"); len(parts) > 1 {
										headers[parts[0]] = matcher.ReplaceAllString(file, strings.TrimSpace(strings.Join(parts[1:], ":")))
									}
								}
							}
							tsize, _ = backendHTTP(target, 0, 1, timeout, headers)
							if match = config.GetString("routes."+route+"."+backend+".cache.match", ""); match != "" && tsize >= 0 {
								if cmatcher := rcache.Get(match); cmatcher != nil && cmatcher.MatchString(file) {
									if path := matcher.ReplaceAllString(file, config.GetString("routes."+route+"."+backend+".cache.path", "")); path != "" {
										select {
										case cacheJobs <- CACHEJOB{"http", target, path, tsize, headers,
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
		status = http.StatusNotFound
		response.WriteHeader(status)
		return
	}
	if tsize == 0 {
		response.Header().Set("Content-Length", "0")
		return
	}

	// parse request bytes-range header and respond initial status
	if header := request.Header.Get("Range"); header != "" {
		if captures := rcache.Get("^bytes=(\\d+)-(\\d*)$").FindStringSubmatch(header); captures != nil {
			if value, err := strconv.ParseInt(captures[1], 10, 64); err == nil {
				start = int(value)
			}
			if value, err := strconv.ParseInt(captures[2], 10, 64); err == nil {
				end = int(value)
			}
		}
	}
	if end < 0 || end >= tsize {
		end = tsize - 1
	}
	if end < start || start >= tsize {
		status = http.StatusRequestedRangeNotSatisfiable
		response.WriteHeader(status)
		return
	}
	response.Header().Set("Content-Length", fmt.Sprintf("%d", end-start+1))
	if start > 0 || end < tsize-1 {
		response.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, tsize))
		status = http.StatusPartialContent
		response.WriteHeader(status)
	}
	if request.Method == http.MethodHead {
		return
	}

	// send requested content
	sstart = time.Now()
	toffset := start
	for {
		bsize := int(math.Min(float64(end-toffset+1), float64(config.GetSizeBounds("server.block_size", 2<<20, 1<<20, 16<<20))))
		if bsize <= 0 {
			break
		}
		if mode == "http" && ftarget != "" {
			if info, err := os.Stat(ftarget); err == nil && info.Mode().IsRegular() && int(info.Size()) == tsize {
				mode, target = "file", ftarget
			}
		}
		switch mode {
		case "file":
			_, content = backendFile(target, toffset, bsize)
		case "http":
			_, content = backendHTTP(target, toffset, bsize, timeout, headers)
		case "exec":
			content = content[start : start+bsize]
		}
		if size, err := response.Write(content); err != nil {
			break
		} else {
			toffset += size
			sent += size
		}
	}
}
