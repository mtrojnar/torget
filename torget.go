package main

/*
	torget 1.0, a fast large file downloader over locally installed Tor
	Copyright © 2021 Michał Trojnara <Michal.Trojnara@stunnel.org>

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
	total           int64
	circuits        int
	timeoutHttp     time.Duration
	timeoutDownload time.Duration
	chunks          []chunk
	done            chan int
	mutex           sync.Mutex
}

const torBlock = 8000 // the longest plain text block in Tor

func NewState(circuits int, timeoutHttp int, timeoutDownload int) *State {
	var s State
	s.circuits = circuits
	s.timeoutHttp = time.Duration(timeoutHttp) * time.Second
	s.timeoutDownload = time.Duration(timeoutDownload) * time.Second
	s.chunks = make([]chunk, s.circuits)
	s.done = make(chan int)
	return &s
}

func (s *State) fetchChunk(id int) {
	defer func() {
		s.done <- id
	}()
	s.mutex.Lock()
	start := s.chunks[id].start
	length := s.chunks[id].length
	s.mutex.Unlock()
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
	user := fmt.Sprintf("tg%d", s.chunks[id].circuit)
	proxyUrl, _ := url.Parse("socks5://" + user + ":" + user + "@127.0.0.1:9050/")
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)}}
	req, _ := http.NewRequestWithContext(ctx, "GET", s.src, nil)
	header := fmt.Sprintf("bytes=%d-%d", start, start+length-1)
	req.Header.Add("Range", header)
	resp, err := client.Do(req)
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	if resp.Body == nil {
		fmt.Println("No response body")
		return
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPartialContent {
		fmt.Println("Unexpected HTTP status:", resp.StatusCode)
		return
	}

	// open the output file
	file, err := os.OpenFile(s.dst, os.O_WRONLY, 0)
	defer file.Close()
	if err != nil {
		fmt.Println(err.Error())
		return
	}
	_, err = file.Seek(start, io.SeekStart)
	if err != nil {
		fmt.Println(err.Error())
		return
	}

	// copy network data to the output file
	buffer := make([]byte, torBlock)
	for {
		if !timer.Stop() { // cancel() already started
			fmt.Println("Downloading: timeout")
			return
		}
		timer.Reset(s.timeoutDownload)
		n, err := resp.Body.Read(buffer)
		// fmt.Println("writing", id, n)
		if n > 0 {
			file.Write(buffer[:n])
			s.mutex.Lock()
			if int64(n) < s.chunks[id].length {
				s.chunks[id].start += int64(n)
				s.chunks[id].length -= int64(n)
			} else {
				s.chunks[id].length = 0
			}
			s.mutex.Unlock()
			if s.chunks[id].length == 0 {
				break
			}
		}
		if err != nil {
			fmt.Println("Downloading:", err.Error())
			break
		}
	}
}

func (s *State) progress() {
	var prev int64
	for {
		time.Sleep(time.Second)
		curr := s.total
		s.mutex.Lock()
		for id := 0; id < s.circuits; id++ {
			curr -= s.chunks[id].length
		}
		s.mutex.Unlock()
		if curr == prev {
			fmt.Printf("%6.2f%% done, stalled\n",
				100*float32(curr)/float32(s.total))
		} else {
			speed := float32(curr-prev) / 1000
			prefix := "K"
			if speed >= 1000 {
				speed /= 1000
				prefix = "M"
			}
			if speed >= 1000 {
				speed /= 1000
				prefix = "G"
			}
			seconds := (s.total - curr) / (curr - prev)
			fmt.Printf("%6.2f%% done, %6.2f %sB/s, ETA %d:%02d:%02d\n",
				100*float32(curr)/float32(s.total),
				speed, prefix,
				seconds/3600, seconds/60%60, seconds%60)
		}
		prev = curr
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
	proxyUrl, _ := url.Parse("socks5://torget:torget@127.0.0.1:9050/")
	client := &http.Client{Transport: &http.Transport{Proxy: http.ProxyURL(proxyUrl)}}
	resp, err := client.Head(s.src)
	if err != nil {
		fmt.Println(err.Error())
		return 1
	}
	s.total = resp.ContentLength
	if s.total <= 0 {
		fmt.Println("Failed to retrieve download length")
		return 1
	}
	fmt.Println("Download length:", s.total, "bytes")

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
	chunkLen := s.total / int64(s.circuits)
	seq := 0
	for id := 0; id < s.circuits; id++ {
		s.chunks[id].start = int64(id) * chunkLen
		s.chunks[id].length = chunkLen
		s.chunks[id].circuit = seq
		seq++
	}
	s.chunks[s.circuits-1].length += s.total % int64(s.circuits)

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
			// fmt.Println("resume", s.chunks[id].length)
		} else { // completed
			longest := 0
			s.mutex.Lock()
			for i := 1; i < s.circuits; i++ {
				if s.chunks[i].length > s.chunks[longest].length {
					longest = i
				}
			}
			s.mutex.Unlock()
			// fmt.Println("completed", s.chunks[longest].length)
			if s.chunks[longest].length == 0 { // all done
				break
			}
			if s.chunks[longest].length <= 5*torBlock { // too short to split
				continue
			}
			// this circuit is faster, so we split 80%/20%
			s.mutex.Lock()
			s.chunks[id].length = s.chunks[longest].length * 4 / 5
			s.chunks[longest].length -= s.chunks[id].length
			s.chunks[id].start = s.chunks[longest].start + s.chunks[longest].length
			s.mutex.Unlock()
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
		fmt.Fprintln(os.Stderr, "torget 1.0, a fast large file downloader over locally installed Tor")
		fmt.Fprintln(os.Stderr, "Copyright © 2021 Michał Trojnara <Michal.Trojnara@stunnel.org>")
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
