package main

import (
	"crypto/tls"
	"flag"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
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

	proxy := &httputil.ReverseProxy{
		Director: func(outReq *http.Request) {
			outReq.URL.Scheme = "http"
			outReq.URL.Host = *host
		},
		Transport:  transport,
		BufferPool: newBufferPool(),
	}

	tcpLn, err := net.Listen("tcp", ":443")
	if err != nil {
		log.Fatal(err)
	}
	keepAliveListener := tcpKeepAliveListener{tcpLn.(*net.TCPListener)}
	srv := http.Server{
		Handler: proxy,
		TLSConfig: &tls.Config{
			MinVersion:    tls.VersionTLS13,
			Renegotiation: tls.RenegotiateNever,
		},
	}
	cert, err := GenerateCert()
	if err != nil {
		log.Fatal(err)
	}

	ln := tls.NewListener(keepAliveListener, &tls.Config{
		Certificates: []tls.Certificate{*cert},
	})

	go func () {
		err := srv.Serve(ln)
		if err != nil {
			log.Fatal(err)
		}
	}()

	log.Fatal(http.ListenAndServe(":80", proxy))
}

type tcpKeepAliveListener struct {
	*net.TCPListener
}

func (ln tcpKeepAliveListener) Accept() (net.Conn, error) {
	tc, err := ln.AcceptTCP()
	if err != nil {
		return nil, err
	}

	if err = tc.SetKeepAlive(true); err != nil {
		return nil, err
	}

	if err = tc.SetKeepAlivePeriod(3 * time.Minute); err != nil {
		return nil, err
	}

	return tc, nil
}
