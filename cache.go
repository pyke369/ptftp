package main

import (
	"fmt"
	"math"
	"math/rand"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/pyke369/golang-support/rcache"
)

type CACHEJOB struct {
	Trigger     string
	Remote      string
	Local       string
	Size        int
	Headers     map[string]string
	Delay       time.Duration
	Concurrency int
	Refresh     int
}

var (
	CacheJobs   chan CACHEJOB
	cacheClient = &http.Client{Timeout: time.Hour}
)

func CacheHandler() {
	workers := int(Config.GetIntegerBounds("server.cache_workers", 64, 1, 64))
	CacheJobs = make(chan CACHEJOB, workers*8)
	for index := 1; index <= workers; index++ {
		go func() {
			for {
				job := <-CacheJobs
				root := filepath.Dir(job.Local)
				target := filepath.Join(root, "_"+filepath.Base(job.Local))
				if _, err := os.Stat(target); err == nil {
					continue
				}
				time.Sleep(job.Delay + (job.Delay / 10) - time.Duration(rand.Int63n(int64(job.Delay/5))))
				if _, err := os.Stat(target); err == nil {
					continue
				}

				Logger.Info(map[string]interface{}{"scope": "cache", "trigger": job.Trigger, "event": "request", "remote": job.Remote, "local": job.Local})
				if err := os.MkdirAll(root, 0755); err != nil {
					Logger.Warn(map[string]interface{}{"scope": "cache", "trigger": job.Trigger, "event": "error", "remote": job.Remote, "local": job.Local, "error": fmt.Sprintf("%v", err)})
					continue
				}

				if handle, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0644); err != nil {
					Logger.Warn(map[string]interface{}{"scope": "cache", "trigger": job.Trigger, "event": "error", "remote": job.Remote, "local": job.Local, "error": fmt.Sprintf("%v", err)})
				} else {
					received, start := 0, time.Now()
					if job.Size > 0 {
						clients := job.Size / int(Config.GetSizeBounds("server.block_size", 2<<20, 1<<20, 16<<20))
						clients = int(math.Min(float64(job.Concurrency), math.Max(1, float64(clients))))
						waiter := make(chan [2]int, 1)
						for client := 0; client < clients; client++ {
							go func(index, count, size int) {
								received, begin, end := 0, (size/count)*index, ((size/count)*(index+1))-1
								if size-end < size/count {
									end = size - 1
								}
								request, _ := http.NewRequest(http.MethodGet, job.Remote, nil)
								request.Header.Add("User-Agent", fmt.Sprintf("%s/%s", PROGNAME, VERSION))
								request.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", begin, end))
								if job.Headers != nil {
									for name, value := range job.Headers {
										request.Header.Add(name, value)
									}
								}
								if response, err := cacheClient.Do(request); err == nil {
									if response.StatusCode/100 == 2 {
										if int(response.ContentLength) == end-begin+1 {
											if captures := rcache.Get(`^bytes (\d+)-(\d+)/(\d+)$`).FindStringSubmatch(response.Header.Get("Content-Range")); captures != nil {
												rbegin, _ := strconv.ParseInt(captures[1], 10, 64)
												rend, _ := strconv.ParseInt(captures[2], 10, 64)
												rsize, _ := strconv.ParseInt(captures[3], 10, 64)
												if int(rbegin) == begin && int(rend) == end && int(rsize) == size {
													var block = make([]byte, 64<<10)
													for received < end-begin+1 {
														read, err := response.Body.Read(block)
														if read > 0 {
															if written, err := handle.WriteAt(block[:read], int64(begin+received)); written == read && err == nil {
																received += read
															}
														}
														if err != nil || read <= 0 {
															break
														}
													}
												}
											}
										}
									}
									response.Body.Close()
								}
								waiter <- [2]int{index, received}
							}(client, clients, job.Size)
						}
						for clients > 0 {
							client := <-waiter
							clients--
							received += client[1]
						}
					}
					handle.Close()

					if int(received) != job.Size {
						Logger.Warn(map[string]interface{}{"scope": "cache", "trigger": job.Trigger, "event": "error", "remote": job.Remote, "local": job.Local,
							"error": fmt.Sprintf("transfer failed (size:%d received:%d)", job.Size, received)})
					} else {
						os.Rename(target, job.Local)
						if job.Refresh != 0 {
							os.WriteFile(filepath.Join(root, "."+filepath.Base(job.Local)+".refresh"), []byte(fmt.Sprintf("%d\n", job.Refresh)), 0644)
						}
						duration := time.Since(start)
						Logger.Info(map[string]interface{}{"scope": "cache", "trigger": job.Trigger, "event": "completion", "remote": job.Remote, "local": job.Local,
							"size": job.Size, "duration": ClientDuration(duration), "bandwidth": ClientBandwidth(float64(job.Size) / (float64(duration) / float64(time.Second)))})
					}
				}
			}
		}()
	}
}
