package cache

import (
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/pyke369/golang-support/file"
	"github.com/pyke369/golang-support/uconfig"
	"github.com/pyke369/golang-support/ulog"
	"github.com/pyke369/golang-support/ustr"

	b "ptftp/backend"
)

type Job struct {
	Trigger     string
	Remote      string
	Local       string
	Headers     map[string]string
	Delay       time.Duration
	Concurrency int
	Refresh     int
}

var (
	jobs chan *Job
)

func Queue(job *Job) {
	select {
	case jobs <- job:

	default:
	}
}

func Run(config *uconfig.UConfig, logger *ulog.ULog) {
	workers := int(config.IntegerBounds("cache_workers", 32, 1, 32))
	jobs = make(chan *Job, workers*8)
	for index := 1; index <= workers; index++ {
		go func() {
			for {
				job := <-jobs
				job.Trigger, job.Remote, job.Local = strings.TrimSpace(job.Trigger), strings.TrimSpace(job.Remote), strings.TrimSpace(job.Local)
				if job.Trigger == "" || job.Local == "" || job.Remote == "" {
					continue
				}

				root := filepath.Dir(job.Local)
				target := filepath.Join(root, "_"+filepath.Base(job.Local))
				if info, err := os.Stat(target); err == nil {
					if time.Since(info.ModTime()) < 5*time.Minute {
						continue
					}
					os.Remove(target)
				}
				if job.Delay != 0 {
					time.Sleep(job.Delay + (job.Delay / 10) - time.Duration(rand.Int63n(int64(job.Delay/5))))
				}
				if _, err := os.Stat(target); err == nil {
					continue
				}
				if err := os.MkdirAll(root, 0o755); err != nil {
					continue
				}
				size, _, err := b.HTTP(job.Remote, 0, 1, 10, nil)
				if err != nil {
					continue
				}
				handle, err := os.OpenFile(target, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
				if err != nil {
					continue
				}
				if size == 0 {
					handle.Close()
					continue
				}
				logger.Info(map[string]any{"scope": "cache", "event": "start", "trigger": job.Trigger, "remote": job.Remote, "local": job.Local, "size": size})

				start, received, clients, waiter := time.Now(), int64(0), max(1, min(int64(job.Concurrency), size/config.SizeBounds("block_size", 4<<20, 1<<20, 16<<20))), make(chan int64, 1)
				for client := int64(0); client < clients; client++ {
					begin, length := (size/clients)*client, size/clients
					if size-(begin+length) < size/clients {
						length = size - begin
					}

					go func(begin, length int64) {
						received, _, err := b.HTTP(job.Remote, begin, length, 3600, job.Headers, handle)
						if err != nil {
							waiter <- 0
							return
						}
						waiter <- received
					}(begin, length)
				}
				for clients > 0 {
					received += <-waiter
					clients--
				}
				close(waiter)
				handle.Close()

				if received != size {
					logger.Warn(map[string]any{"scope": "cache", "event": "end", "trigger": job.Trigger, "remote": job.Remote, "local": job.Local,
						"size": size, "reason": "received " + strconv.FormatInt(received, 10)})
					os.Remove(target)

				} else {
					os.Rename(target, job.Local)
					if job.Refresh != 0 {
						file.Write(filepath.Join(root, "."+filepath.Base(job.Local)+".refresh"), []string{strconv.Itoa(job.Refresh)})
					}
					duration := time.Since(start)
					logger.Info(map[string]any{"scope": "cache", "event": "end", "trigger": job.Trigger, "remote": job.Remote, "local": job.Local,
						"size": size, "duration": ustr.Duration(duration), "bandwidth": ustr.Bandwidth(int64(float64(size*8) / (float64(duration) / float64(time.Second))))})
				}
			}
		}()
	}
}
