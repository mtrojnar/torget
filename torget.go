package main

/*
	torget 1.0, a fast large file downloader over locally installed Tor
	Copyright © 2021-2023 Michał Trojnara <Michal.Trojnara@stunnel.org>

	This program is free software: you can redistribute it and/or modify
	it under the terms of the GNU General Public License as published by
	the Free Software Foundation, either version 3 of the License.

	This program is distributed in the hope that it will be useful,
	but WITHOUT ANY WARRANTY; without even the implied warranty of
	MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
	GNU General Public License for more details.

	You should have received a copy of the GNU General Public License
	along with this program.  If not, see <https://www.gnu.org/licenses/>.
*/

import (
	"context"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"time"
)

type chunk struct {
	start   int64
	length  int64
	circuit int
}

type State struct {
	src             string
	dst             string
	bytesTotal      int64
	bytesPrev       int64
	circuits        int
	timeoutHttp     time.Duration
	timeoutDownload time.Duration
	chunks          []chunk
	done            chan int
	log             chan string
	rwmutex         sync.RWMutex
}

const torBlock = 8000 // the longest plain text block in Tor

func NewState(circuits int, timeoutHttp int, timeoutDownload int) *State {
	var s State
	s.circuits = circuits
	s.timeoutHttp = time.Duration(timeoutHttp) * time.Second
	s.timeoutDownload = time.Duration(timeoutDownload) * time.Second
	s.chunks = make([]chunk, s.circuits)
	s.done = make(chan int)
	s.log = make(chan string, 10)
	return &s
}

func httpClient(user string) *http.Client {
	proxyUrl, _ := url.Parse("socks5://" + user + ":" + user + "@127.0.0.1:9050/")
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)},
	}
}

func (s *State) fetchChunk(id int) {
	defer func() {
		s.done <- id
	}()
	s.rwmutex.RLock()
	start := s.chunks[id].start
	length := s.chunks[id].length
	s.rwmutex.RUnlock()
	if length == 0 {
		return
	}

	// make an HTTP request in a new circuit
	ctx, cancel := context.WithCancel(context.TODO())
	timer := time.AfterFunc(s.timeoutHttp, func() {
		cancel()
	})
	defer func() {
		if timer.Stop() {
			cancel() // make sure cancel() is executed exactly once
		}
	}()
	client := httpClient(fmt.Sprintf("tg%d", s.chunks[id].circuit))
	req, _ := http.NewRequestWithContext(ctx, "GET", s.src, nil)
	header := fmt.Sprintf("bytes=%d-%d", start, start+length-1)
	req.Header.Add("Range", header)
	resp, err := client.Do(req)
	if err != nil {
		s.log <- fmt.Sprintf("Client Do: %s", err.Error())
		return
	}
	if resp.Body == nil {
		s.log <- "Client Do: No response body"
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		s.log <- fmt.Sprintf("Client Do: Unexpected HTTP status: %d", resp.StatusCode)
		return
	}

	// open the output file
	file, err := os.OpenFile(s.dst, os.O_WRONLY, 0)
	defer file.Close()
	if err != nil {
		s.log <- fmt.Sprintf("os OpenFile: %s", err.Error())
		return
	}
	_, err = file.Seek(start, io.SeekStart)
	if err != nil {
		s.log <- fmt.Sprintf("File Seek: %s", err.Error())
		return
	}

	// copy network data to the output file
	buffer := make([]byte, torBlock)
	for {
		if !timer.Stop() { // cancel() already started
			s.log <- "Timer Stop: timeout"
			return
		}
		timer.Reset(s.timeoutDownload)
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			file.Write(buffer[:n])
			// enough to RLock(), as we only modify our own chunk
			s.rwmutex.RLock()
			if int64(n) < s.chunks[id].length {
				s.chunks[id].start += int64(n)
				s.chunks[id].length -= int64(n)
			} else {
				s.chunks[id].length = 0
			}
			s.rwmutex.RUnlock()
			if s.chunks[id].length == 0 {
				break
			}
		}
		if err != nil {
			s.log <- fmt.Sprintf("ReadCloser Read: %s", err.Error())
			break
		}
	}
}

func (s *State) printLogs() {
	n := len(s.log)
	logs := make([]string, n+1)
	for i := 0; i < n; i++ {
		logs[i] = <-s.log
	}
	logs[n] = "stop" // not an expected log line
	sort.Strings(logs)
	prevLog := "start" // not an expected log line
	cnt := 0
	for _, log := range logs {
		if log == prevLog {
			cnt++
		} else {
			if cnt > 0 {
				if cnt > 1 {
					prevLog = fmt.Sprintf("%s (%d times)", prevLog, cnt)
				}
				fmt.Printf("\r%-40s\n", prevLog)
			}
			prevLog = log
			cnt = 1
		}
	}
}

func (s *State) statusLine() string {
	// calculate bytes transferred since the previous invocation
	curr := s.bytesTotal
	s.rwmutex.RLock()
	for id := 0; id < s.circuits; id++ {
		curr -= s.chunks[id].length
	}
	s.rwmutex.RUnlock()

	var status string
	if curr == s.bytesPrev {
		status = fmt.Sprintf("%6.2f%% done, stalled",
			100*float32(curr)/float32(s.bytesTotal))
	} else {
		speed := float32(curr-s.bytesPrev) / 1000
		prefix := "K"
		if speed >= 1000 {
			speed /= 1000
			prefix = "M"
		}
		if speed >= 1000 {
			speed /= 1000
			prefix = "G"
		}
		seconds := (s.bytesTotal - curr) / (curr - s.bytesPrev)
		status = fmt.Sprintf("%6.2f%% done, %6.2f %sB/s, ETA %d:%02d:%02d",
			100*float32(curr)/float32(s.bytesTotal),
			speed, prefix,
			seconds/3600, seconds/60%60, seconds%60)
	}

	s.bytesPrev = curr
	return status
}

func (s *State) progress() {
	for {
		time.Sleep(time.Second)
		s.printLogs()
		fmt.Printf("\r%-40s", s.statusLine())
	}
}

func (s *State) Fetch(src string) int {
	// setup file name
	s.src = src
	srcUrl, err := url.Parse(src)
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}
	path := srcUrl.EscapedPath()
	slash := strings.LastIndex(path, "/")
	if slash >= 0 {
		s.dst = path[slash+1:]
	} else {
		s.dst = path
	}
	if s.dst == "" {
		s.dst = "index"
	}
	fmt.Println("Output file:", s.dst)

	// get the target length
	client := httpClient("torget")
	resp, err := client.Head(s.src)
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}
	if resp.ContentLength <= 0 {
		fmt.Println("Failed to retrieve download length")
		return 1
	}
	s.bytesTotal = resp.ContentLength
	fmt.Println("Download length:", s.bytesTotal, "bytes")

	// create the output file
	file, err := os.Create(s.dst)
	if file != nil {
		file.Close()
	}
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}

	// initialize chunks
	chunkLen := s.bytesTotal / int64(s.circuits)
	seq := 0
	for id := 0; id < s.circuits; id++ {
		s.chunks[id].start = int64(id) * chunkLen
		s.chunks[id].length = chunkLen
		s.chunks[id].circuit = seq
		seq++
	}
	s.chunks[s.circuits-1].length += s.bytesTotal % int64(s.circuits)

	// spawn initial fetchers
	go s.progress()
	go func() {
		for id := 0; id < s.circuits; id++ {
			go s.fetchChunk(id)
			time.Sleep(499 * time.Millisecond) // be gentle to the local tor daemon
		}
	}()

	// spawn additional fetchers as needed
	for {
		id := <-s.done
		if s.chunks[id].length > 0 { // error
			// resume in a new and hopefully faster circuit
			s.chunks[id].circuit = seq
			seq++
		} else { // completed
			longest := 0
			s.rwmutex.RLock()
			for i := 1; i < s.circuits; i++ {
				if s.chunks[i].length > s.chunks[longest].length {
					longest = i
				}
			}
			s.rwmutex.RUnlock()
			if s.chunks[longest].length == 0 { // all done
				break
			}
			if s.chunks[longest].length <= 5*torBlock { // too short to split
				continue
			}
			// this circuit is faster, so we split 80%/20%
			s.rwmutex.Lock()
			s.chunks[id].length = s.chunks[longest].length * 4 / 5
			s.chunks[longest].length -= s.chunks[id].length
			s.chunks[id].start = s.chunks[longest].start + s.chunks[longest].length
			s.rwmutex.Unlock()
		}
		go s.fetchChunk(id)
	}
	return 0
}

func main() {
	circuits := flag.Int("circuits", 20, "concurrent circuits")
	timeoutHttp := flag.Int("http-timeout", 10, "HTTP timeout (seconds)")
	timeoutDownload := flag.Int("download-timeout", 5, "download timeout (seconds)")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "torget 1.1, a fast large file downloader over locally installed Tor")
		fmt.Fprintln(os.Stderr, "Copyright © 2021-2023 Michał Trojnara <Michal.Trojnara@stunnel.org>")
		fmt.Fprintln(os.Stderr, "Licensed under GNU/GPL version 3")
		fmt.Fprintln(os.Stderr)
		fmt.Fprintln(os.Stderr, "Usage: torget [FLAGS] URL")
		flag.PrintDefaults()
	}
	flag.Parse()
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(1)
	}
	state := NewState(*circuits, *timeoutHttp, *timeoutDownload)
	os.Exit(state.Fetch(flag.Arg(0)))
}

// vim: noet:ts=4:sw=4:sts=4:spell
