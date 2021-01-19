package main

import (
	"flag"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"strings"
	// _ "net/http/pprof"
	"sync"
	"time"
)

const bufferPoolSize = 32 * 1024

func newBufferPool() *bufferPool {
	return &bufferPool{
		pool: sync.Pool{
			New: func() interface{} {
				return make([]byte, bufferPoolSize)
			},
		},
	}
}

type bufferPool struct {
	pool sync.Pool
}

func (b *bufferPool) Get() []byte {
	return b.pool.Get().([]byte)
}
func (b *bufferPool) Put(bytes []byte) {
	b.pool.Put(bytes)
}
func main() {
	// debug.SetGCPercent(1000)
	host := flag.String("backend", "172.17.0.2", "Backend hostname or ip")
	flag.Parse()
	if host == nil {
		log.Fatal("-backend and -addr cannot be nil")
	}
	dialer := &net.Dialer{
		Timeout:   30 * time.Second,
		KeepAlive: 30 * time.Second,
		DualStack: true,
	}
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConnsPerHost:   500,
		DialContext:           dialer.DialContext,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	hosts := make(chan string, 10000)
	go func() {
		split := strings.Split(*host, ",")
		for {
			for _, s := range split {
				hosts <- s
			}
		}
	}()
	var proxy http.Handler
	proxy = &httputil.ReverseProxy{
		Director: func(outReq *http.Request) {
			outReq.URL.Scheme = "http"
			outReq.URL.Host = <-hosts
		},
		Transport:  transport,
		BufferPool: newBufferPool(),
	}

	// keepAliveListener := tcpKeepAliveListener{tcpLn.(*net.TCPListener)}
	// srv := http.Server{
	// 	Handler: proxy,
	// 	TLSConfig: &tls.Config{
	// 		MinVersion:    tls.VersionTLS13,
	// 		Renegotiation: tls.RenegotiateNever,
	// 	},
	// }
	// cert, err := GenerateCert()
	// if err != nil {
	// 	log.Fatal(err)
	// }
	// ln := tls.NewListener(keepAliveListener, &tls.Config{
	// 	Certificates: []tls.Certificate{*cert},
	// })
	// // go func() {
	//     http.DefaultServeMux.Handle("/debug/fgprof", fgprof.Handler())
	//     http.ListenAndServe(":8080", http.DefaultServeMux)
	// }()
	// go func() {
	// 	err := srv.Serve(ln)
	// 	if err != nil {
	// 		log.Fatal(err)
	// 	}
	// }()

	proxy = http.HandlerFunc(func(rw http.ResponseWriter, req *http.Request) {
		start := time.Now()
		req.URL.Scheme = "http"
		req.URL.Host = <-hosts
		fmt.Println("req", time.Since(start))
		resp, err := transport.RoundTrip(req)
		fmt.Println("rt", time.Since(start))
		if err != nil {
			fmt.Fprintf(rw, "%v", err)
			return
		}
		fmt.Println("err", time.Since(start))
		resp.Write(rw)

		fmt.Println("write", time.Since(start))
		rw.(http.Flusher).Flush()
		fmt.Println("flush", time.Since(start))
	})
	log.Fatal(http.ListenAndServe(":8080", proxy))
}
