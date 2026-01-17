package http

import (
	"net/http"
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
	"ptftp/common"
)

func Handler(config *uconfig.UConfig, logger *ulog.ULog) http.Handler {
	return http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {

		file := strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(r.URL.Path, "../", ""), "./", ""), "&", ""), ";", "")
		status, timeout, tsize, mode, target, ftarget, content, headers, env, begin, end, sent := 200, 10, int64(-1), "", "", "", []byte{}, map[string]string{}, []string{}, int64(0), int64(-1), int64(0)
		start := time.Now()
		logger.Info(map[string]any{"scope": "http", "event": "request", "file": file, "remote": r.RemoteAddr})
		defer func() {
			if status/100 > 2 {
				logger.Warn(map[string]any{"scope": "http", "event": "response", "remote": r.RemoteAddr, "file": file, "status": status})

			} else {
				duration := time.Since(start)
				logger.Info(map[string]any{"scope": "http", "event": "response", "remote": r.RemoteAddr, "file": file, "mode": mode,
					"status": status, "size": tsize, "sent": sent, "duration": ustr.Duration(duration),
					"bandwidth": ustr.Bandwidth((sent * 8) / int64(duration) / int64(time.Second))})
			}
		}()
		rw.Header().Set("Server", common.PROGNAME+"/"+common.PROGVER)
		rw.Header().Set("Accept-Ranges", "bytes")
		if r.Method != http.MethodHead && r.Method != http.MethodGet {
			status = http.StatusMethodNotAllowed
			rw.WriteHeader(status)
			return
		}

		// check routes/backends and gather requested file information
		for _, path := range config.Paths("routes") {
			if route := config.String(path); route != "" {
				if match := config.String(config.Path("routes", route, "match")); match != "" {
					if matcher := rcache.Get(match); matcher.MatchString(file) {
						for _, path := range config.Paths(config.Path("routes", route, "backends")) {
							backend := config.String(path)
							mode = strings.ToLower(config.String(config.Path("routes", route, backend, "mode")))
							target = matcher.ReplaceAllString(file, config.String(config.Path("routes", route, backend, "target")))
							switch mode {
							case "file":
								ftarget = target
								tsize, _, _ = b.File(target, 0, 1)

							case "http":
								for _, path := range config.Paths(config.Path("routes", route, backend, "headers")) {
									if value := strings.TrimSpace(config.String(path)); value != "" {
										if parts := strings.Split(value, ":"); len(parts) > 1 {
											headers[parts[0]] = matcher.ReplaceAllString(file, strings.TrimSpace(strings.Join(parts[1:], ":")))
										}
									}
								}
								tsize, _, _ = b.HTTP(target, 0, 1, timeout, headers)
								if tsize >= 0 {
									for _, policy := range config.Strings(config.Path("routes", route, backend, "cache", "policies")) {
										prefix := config.Path("routes", route, backend, "cache", policy)
										if match = config.String(config.Path(prefix, "match")); match != "" {
											if cmatcher := rcache.Get(match); cmatcher != nil && cmatcher.MatchString(file) {
												if path := matcher.ReplaceAllString(file, config.String(config.Path(prefix, "path"))); path != "" {
													c.Queue(&c.Job{
														Trigger:     "http",
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
			status = http.StatusNotFound
			rw.WriteHeader(status)
			return
		}
		if tsize == 0 {
			rw.Header().Set("Content-Length", "0")
			return
		}

		// parse request bytes-range header and respond initial status
		if header := r.Header.Get("Range"); header != "" {
			if captures := rcache.Get("^bytes=(\\d+)-(\\d*)$").FindStringSubmatch(header); captures != nil {
				if value, err := strconv.ParseInt(captures[1], 10, 64); err == nil {
					begin = value
				}
				if value, err := strconv.ParseInt(captures[2], 10, 64); err == nil {
					end = value
				}
			}
		}
		if end < 0 || end >= tsize {
			end = tsize - 1
		}
		if end < begin || begin >= tsize {
			status = http.StatusRequestedRangeNotSatisfiable
			rw.WriteHeader(status)
			return
		}
		rw.Header().Set("Content-Length", strconv.FormatInt(end-begin+1, 10))
		if begin > 0 || end < tsize-1 {
			rw.Header().Set("Content-Range", "bytes "+strconv.FormatInt(begin, 10)+"-"+strconv.FormatInt(end, 10)+"/"+strconv.FormatInt(tsize, 10))
			status = http.StatusPartialContent
			rw.WriteHeader(status)
		}
		if r.Method == http.MethodHead {
			return
		}

		// send requested content
		start = time.Now()
		toffset := begin
		for {
			bsize := min(end-toffset+1, config.SizeBounds("block_size", 4<<20, 1<<20, 16<<20))
			if bsize <= 0 {
				break
			}
			if mode == "http" && ftarget != "" {
				if info, err := os.Stat(ftarget); err == nil && info.Mode().IsRegular() && info.Size() == tsize {
					mode, target = "file", ftarget
				}
			}
			switch mode {
			case "file":
				_, content, _ = b.File(target, toffset, bsize)

			case "http":
				_, content, _ = b.HTTP(target, toffset, bsize, timeout, headers)

			case "exec":
				content = content[begin : begin+bsize]
			}
			if size, err := rw.Write(content); err != nil {
				break

			} else {
				toffset += int64(size)
				sent += int64(size)
			}
		}
	})
}
