package main

import (
	"fmt"
	"io/ioutil"
	"math"
	"net/http"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pyke369/golang-support/rcache"
)

func fileBackend(target string, offset, length int) (total int, content []byte) {
	defer func() {
		logger.Debug("FILE %s [ %d - %d ] > %d / %d", target, offset, offset+length-1, total, len(content))
	}()
	total = -1
	if info, err := os.Stat(target); err == nil && info.Mode().IsRegular() {
		total = int(info.Size())
		if offset < total {
			if handle, err := os.Open(target); err == nil {
				if offset+length > total {
					length = total - offset
				}
				content = make([]byte, length)
				read, _ := handle.ReadAt(content, int64(offset))
				content = content[:read]
				handle.Close()
			}
		}
	}
	return total, content
}

func httpBackend(target string, offset, length, timeout int, headers map[string]string) (total int, content []byte) {
	defer func() {
		logger.Debug("HTTP %s [ %d - %d ] > %d / %d", target, offset, offset+length-1, total, len(content))
	}()
	total = -1
	request, _ := http.NewRequest(http.MethodGet, target, nil)
	request.Header.Add("User-Agent", fmt.Sprintf("%s/%s", progname, version))
	request.Header.Add("Range", fmt.Sprintf("bytes=%d-%d", offset, offset+length-1))
	if headers != nil {
		for name, value := range headers {
			request.Header.Add(name, value)
		}
	}
	client := &http.Client{Timeout: time.Duration(timeout) * time.Second}
	if response, err := client.Do(request); err == nil {
		if code := response.StatusCode; code/100 == 2 {
			total = int(math.Max(float64(response.ContentLength), 0))
			crange := response.Header.Get("Content-Range")
			if code == http.StatusPartialContent && crange != "" {
				if captures := rcache.Get("^bytes \\d+-\\d+/(\\d+)$").FindStringSubmatch(crange); captures != nil {
					total, _ = strconv.Atoi(captures[1])
				}
			}
			content, _ = ioutil.ReadAll(response.Body)
			if total == 0 {
				total = len(content)
			}
		}
		response.Body.Close()
	}
	return total, content
}

func execBackend(target string, timeout int, env []string) (total int, content []byte) {
	defer func() {
		logger.Debug("EXEC %s > %d / %d", target, total, len(content))
	}()
	total = -1
	parts := strings.Split(target, " ")
	command := &exec.Cmd{Path: parts[0], Args: parts, Env: append(os.Environ(), env...)}
	if stdout, err := command.StdoutPipe(); err == nil {
		if err := command.Start(); err == nil {
			done := make(chan bool)
			go func() {
				content, _ = ioutil.ReadAll(stdout)
				done <- true
			}()
			select {
			case <-done:
			case <-time.After(time.Duration(timeout) * time.Second):
				syscall.Kill(command.Process.Pid, syscall.SIGKILL)
			}
			if command.Wait() == nil {
				total = len(content)
			}
		}
	}
	return total, content
}
