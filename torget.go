package main

/*
	torget is a fast large file downloader over locally installed Tor
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
	bytes   int64
	since   time.Time
	cancel  context.CancelFunc
}

type State struct {
	ctx         context.Context
	src         string
	dst         string
	bytesTotal  int64
	bytesPrev   int64
	circuits    int
	minLifetime time.Duration
	verbose     bool
	chunks      []chunk
	done        chan int
	log         chan string
	terminal    bool
	rwmutex     sync.RWMutex
}

const torBlock = 8000 // the longest plain text block in Tor

func httpClient(user string) *http.Client {
	proxyUrl, _ := url.Parse("socks5://" + user + ":" + user + "@127.0.0.1:9050/")
	return &http.Client{
		Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)},
	}
}

func NewState(ctx context.Context, circuits int, minLifetime int, verbose bool) *State {
	var s State
	s.circuits = circuits
	s.minLifetime = time.Duration(minLifetime) * time.Second
	s.verbose = verbose
	s.chunks = make([]chunk, s.circuits)
	s.ctx = ctx
	s.done = make(chan int)
	s.log = make(chan string, 10)
	st, _ := os.Stdout.Stat()
	s.terminal = st.Mode()&os.ModeCharDevice == os.ModeCharDevice
	return &s
}

func (s *State) printPermanent(txt string) {
	if s.terminal {
		fmt.Printf("\r%-40s\n", txt)
	} else {
		fmt.Println(txt)
	}
}

func (s *State) printTemporary(txt string) {
	if s.terminal {
		fmt.Printf("\r%-40s", txt)
	}
}

func (s *State) chunkInit(id int) (client *http.Client, req *http.Request) {
	s.chunks[id].bytes = 0
	s.chunks[id].since = time.Now()
	ctx, cancel := context.WithCancel(s.ctx)
	s.chunks[id].cancel = cancel
	client = httpClient(fmt.Sprintf("tg%d", s.chunks[id].circuit))
	req, _ = http.NewRequestWithContext(ctx, "GET", s.src, nil)
	req.Header.Add("Range", fmt.Sprintf("bytes=%d-%d",
		s.chunks[id].start, s.chunks[id].start+s.chunks[id].length-1))
	return
}

func (s *State) chunkFetch(id int, client *http.Client, req *http.Request) {
	defer func() {
		s.done <- id
	}()

	if s.verbose {
		err := s.getExitNode(id, client)
		if err != nil {
			s.log <- fmt.Sprintf("getExitNode: %s", err.Error())
		}
	}

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
	_, err = file.Seek(s.chunks[id].start, io.SeekStart)
	if err != nil {
		s.log <- fmt.Sprintf("File Seek: %s", err.Error())
		return
	}

	// copy network data to the output file
	buffer := make([]byte, torBlock)
	for {
		n, err := resp.Body.Read(buffer)
		if n > 0 {
			file.Write(buffer[:n])
			// enough to RLock(), as we only modify our own chunk
			s.rwmutex.RLock()
			if int64(n) < s.chunks[id].length {
				s.chunks[id].start += int64(n)
				s.chunks[id].length -= int64(n)
				s.chunks[id].bytes += int64(n)
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

func (s *State) getExitNode(id int, client *http.Client) error {
	req, err := http.NewRequest(http.MethodGet, "https://check.torproject.org/api/ip", nil)
	if err != nil {
		return fmt.Errorf("http NewRequest: %s", err.Error())
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("client Do: %s", err.Error())
	}
	if resp.StatusCode != 200 {
		return fmt.Errorf("client Do: Unexpected HTTP status: %d", resp.StatusCode)
	}
	if resp.Body == nil {
		return fmt.Errorf("client Do: No response body")
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return fmt.Errorf("io ReadAll: %s", err.Error())
	}

	s.log <- fmt.Sprintf("Circuit #%d: Exit node: %s", id, body)
	return nil
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
				s.printPermanent(prevLog)
			}
			prevLog = log
			cnt = 1
		}
	}
}

func (s *State) ignoreLogs() {
	for len(s.log) > 0 {
		<-s.log
	}
}

func (s *State) statusLine() (status string) {
	// calculate bytes transferred since the previous invocation
	curr := s.bytesTotal
	s.rwmutex.RLock()
	for id := 0; id < s.circuits; id++ {
		curr -= s.chunks[id].length
	}
	s.rwmutex.RUnlock()

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
	return
}

func (s *State) progress() {
	for {
		time.Sleep(time.Second)
		if s.verbose {
			s.printLogs()
		} else {
			s.ignoreLogs()
		}
		s.printTemporary(s.statusLine())
	}
}

func (s *State) darwin() { // kill the worst performing circuit
	victim := -1
	var slowest float64
	now := time.Now()

	s.rwmutex.RLock()
	for id := 0; id < s.circuits; id++ {
		if s.chunks[id].cancel == nil {
			continue
		}
		eplased := now.Sub(s.chunks[id].since)
		if eplased < s.minLifetime {
			continue
		}
		throughput := float64(s.chunks[id].bytes) / eplased.Seconds()
		if victim >= 0 && throughput >= slowest {
			continue
		}
		victim = id
		slowest = throughput
	}
	if victim >= 0 {
		// fmt.Printf("killing %5.1fs %5.1fkB/s",
		//	now.Sub(s.chunks[victim].since).Seconds(), slowest/1024.0)
		s.chunks[victim].cancel()
		s.chunks[victim].cancel = nil
	}
	s.rwmutex.RUnlock()
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
			client, req := s.chunkInit(id)
			go s.chunkFetch(id, client, req)
			time.Sleep(499 * time.Millisecond) // be gentle to the local tor daemon
		}
	}()

	// spawn additional fetchers as needed
	for {
		select {
		case id := <-s.done:
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
					s.printPermanent("Download complete")
					return 0
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
			client, req := s.chunkInit(id)
			go s.chunkFetch(id, client, req)
		case <-time.After(time.Second * 30):
			s.darwin()
		}
	}
}

func main() {
	circuits := flag.Int("circuits", 20, "concurrent circuits")
	minLifetime := flag.Int("min-lifetime", 10, "minimum circuit lifetime (seconds)")
	verbose := flag.Bool("verbose", false, "diagnostic details")
	flag.Usage = func() {
		fmt.Fprintln(os.Stderr, "torget 2.0, a fast large file downloader over locally installed Tor")
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
	ctx := context.Background()
	state := NewState(ctx, *circuits, *minLifetime, *verbose)
	context.Background()
	os.Exit(state.Fetch(flag.Arg(0)))
}

// vim: noet:ts=4:sw=4:sts=4:spell
