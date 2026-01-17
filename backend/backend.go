package backend

import (
	"crypto/tls"
	"errors"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/pyke369/golang-support/file"
	"github.com/pyke369/golang-support/rcache"

	"ptftp/common"
)

func File(source string, offset, length int64) (total int64, content []byte, err error) {
	total = -1

	info, err := os.Stat(source)
	if err != nil {
		return total, content, err
	}
	if !info.Mode().IsRegular() {
		return total, content, errors.New("not a regular file")
	}
	if lines := file.Read(filepath.Join(filepath.Dir(source), "."+filepath.Base(source)+".refresh")); len(lines) != 0 {
		if refresh, err := strconv.Atoi(lines[0]); err == nil && refresh > 0 && int(time.Since(info.ModTime())/time.Second) >= refresh {
			os.Remove(source)
		}
	}
	total = info.Size()
	if offset+length > total {
		length = total - offset
	}

	handle, err := os.Open(source)
	if err != nil {
		return total, content, err
	}
	defer handle.Close()
	content = make([]byte, length)
	read, err := handle.ReadAt(content, offset)
	content = content[:read]
	if err != nil {
		return total, content, err
	}

	return total, content, nil
}

func HTTP(source string, offset, length int64, timeout int, headers map[string]string, target ...*os.File) (total int64, content []byte, err error) {
	total = -1

	request, _ := http.NewRequest(http.MethodGet, source, http.NoBody)
	request.Header.Add("User-Agent", common.PROGNAME+"/"+common.PROGVER)
	request.Header.Add("Range", "bytes="+strconv.FormatInt(offset, 10)+"-"+strconv.FormatInt(offset+length-1, 10))
	for name, value := range headers {
		request.Header.Add(name, value)
	}

	client := &http.Client{
		Timeout:   time.Duration(timeout) * time.Second,
		Transport: &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}},
	}
	response, err := client.Do(request)
	if err != nil {
		return total, content, err
	}
	defer response.Body.Close()

	if response.StatusCode != http.StatusPartialContent {
		return total, content, errors.New("http status " + strconv.Itoa(response.StatusCode))
	}
	captures := rcache.Get(`^bytes (\d+)-(\d+)/(\d+)$`).FindStringSubmatch(response.Header.Get("Content-Range"))
	if captures == nil {
		return total, content, errors.New("missing content-range header")
	}
	if len(target) != 0 && target[0] != nil {
		begin, _ := strconv.ParseInt(captures[1], 10, 64)
		end, _ := strconv.ParseInt(captures[2], 10, 64)
		if begin != offset || end != begin+length-1 {
			return total, content, errors.New("invalid range returned")
		}
		total = end - begin + 1

		content = make([]byte, 64<<10)
		for begin < end {
			read, err := response.Body.Read(content)
			if read > 0 {
				if _, err := target[0].WriteAt(content[:read], begin); err != nil {
					return total, nil, err
				}
				begin += int64(read)
			}
			if err != nil {
				break
			}
		}
		if begin != end+1 {
			return total, nil, errors.New("invalid content size")
		}

	} else {
		total, _ = strconv.ParseInt(captures[3], 10, 64)
		content, _ = io.ReadAll(response.Body)
	}

	return total, content, nil
}

func Exec(source string, timeout int, env []string) (total int64, content []byte, err error) {
	total = -1

	parts := strings.Split(source, " ")
	command := &exec.Cmd{Path: parts[0], Args: parts, Env: append(os.Environ(), env...)}
	stdout, err := command.StdoutPipe()
	if err != nil {
		return total, content, err
	}
	err = command.Start()
	if err != nil {
		return total, content, err
	}

	done := make(chan bool)
	go func() {
		content, _ = io.ReadAll(stdout)
		done <- true
	}()
	select {
	case <-done:

	case <-time.After(time.Duration(timeout) * time.Second):
		syscall.Kill(command.Process.Pid, syscall.SIGKILL)
		return total, content, errors.New("timeout")
	}
	if command.Wait() == nil {
		total = int64(len(content))
	}

	return total, content, nil
}
