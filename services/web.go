package services

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/pkg/errors"

	log "github.com/sirupsen/logrus"
	"github.com/urfave/cli"
)

const (
	webHostFlag     = "host"
	webUpstreamFlag = "upstream"
	webPortFlag     = "port"
	retries         = 10
	retryInterval   = 50
)

type Web struct {
	host     string
	port     int
	upstream string
	ln       net.Listener
}

func NewWeb(c *cli.Context) *Web {
	return &Web{
		host:     c.String(webHostFlag),
		port:     c.Int(webPortFlag),
		upstream: c.String(webUpstreamFlag),
	}
}

func RegisterWebFlags(f []cli.Flag) []cli.Flag {
	return append(f,
		cli.StringFlag{
			Name:   webHostFlag,
			Usage:  "listening host",
			Value:  "",
			EnvVar: "WEB_HOST",
		},
		cli.StringFlag{
			Name:   webUpstreamFlag,
			Usage:  "upstream",
			Value:  "",
			EnvVar: "UPSTREAM",
		},
		cli.IntFlag{
			Name:   webPortFlag,
			Usage:  "http listening port",
			Value:  8080,
			EnvVar: "WEB_PORT",
		},
	)
}

type MyTransport struct {
	http.Transport
}

func (s *MyTransport) RoundTrip(req *http.Request) (resp *http.Response, err error) {
	r := 0
	ri := retryInterval
	for {
		resp, err = s.Transport.RoundTrip(req)
		if req.Context().Err() != nil {
			log.WithError(err).Info("got context error")
			break
		} else if err != nil && r < retries {
			log.WithError(err).Info("got roundtrip error")
			<-time.After(time.Duration(ri) * time.Millisecond)
			r++
			ri *= 2
		} else {
			break
		}
	}
	return resp, err
}

func NewMyTransport() *MyTransport {
	return &MyTransport{
		Transport: http.Transport{
			Proxy: http.ProxyFromEnvironment,
			DialContext: (&net.Dialer{
				Timeout:   30 * time.Second,
				KeepAlive: 600 * time.Second,
			}).DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          100,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   10 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
		},
	}
}

func serveWithoutPanic(h http.Handler, w http.ResponseWriter, r *http.Request) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err, _ = r.(error)
		}
	}()
	h.ServeHTTP(w, r)
	return
}

func finalizeRequest(cl *http.Client, etag string, start, end int, w http.ResponseWriter, r *http.Request) error {
	endStr := ""
	if end != -1 {
		endStr = fmt.Sprintf("%v", end)
	}
	r.Header.Set("Range", fmt.Sprintf("bytes=%v-%v", start, endStr))
	// log.Infof("making finalize request=%v", r)
	resp, err := cl.Do(r)
	if err != nil {
		return errors.Wrapf(err, "failed to perform request=%+v", r)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 502 {
		return errors.Errorf("got bad status code=%v", resp.StatusCode)
	}
	if resp.Header.Get("Etag") == "" || (etag != "" && resp.Header.Get("Etag") != etag) {
		log.Warnf("etag changed old=%v new=%v", etag, resp.Header.Get("Etag"))
		return nil
	}
	_, err = io.Copy(w, resp.Body)
	if err != nil {
		return errors.Wrapf(err, "failed to copy body of request=%+v", r)
	}
	return nil
}

func retryHandler(cl *http.Client, re *url.URL, h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// log.Infof("got request %v", r)
		wi := NewResponseWrtierInterceptor(w)
		err := serveWithoutPanic(h, wi, r)
		if err != http.ErrAbortHandler && wi.statusCode < 502 {
			// log.WithError(err).Warnf("got error with status code %v", wi.statusCode)
			return
		}
		ar := wi.Header().Get("Accept-Ranges")
		et := wi.Header().Get("Etag")
		if err != nil {
			log.WithError(err).Warn("got abort error")
		}
		if wi.statusCode >= 500 {
			log.Warnf("got status code=%v", wi.statusCode)
		}
		if (ar != "" && et != "") || wi.statusCode >= 502 {
			start := 0
			end := -1
			ra := r.Header.Get("Range")
			if ra != "" {
				parts := strings.Split(strings.TrimPrefix(ra, "bytes="), "-")
				if len(parts) != 2 {
					log.Warnf("failed to parse %v", ra)
					return
				}
				start, err = strconv.Atoi(parts[0])
				if err != nil {
					log.WithError(err).Warnf("failed to parse %v", ra)
					return
				}
				if parts[1] != "" {
					end, err = strconv.Atoi(parts[1])
					if err != nil {
						log.WithError(err).Warnf("failed to parse %v", ra)
						return
					}
				}
			}

			rrr := r.Clone(r.Context())
			rrr.URL.Host = re.Host
			rrr.URL.Scheme = re.Scheme
			rrr.RequestURI = ""
			rrr.Host = re.Host

			err = nil
			rr := 0
			ow := 0
			ri := retryInterval
			for {
				err = finalizeRequest(cl, et, start+wi.bytesWritten, end, wi, rrr)
				if wi.bytesWritten > ow {
					rr = 0
					ri = retryInterval
					ow = wi.bytesWritten
				}
				if r.Context().Err() != nil {
					log.WithError(r.Context().Err()).Warn("got context error")
					break
				} else if err != nil && rr < retries {
					log.WithError(err).Warn("got finalize error")
					rr++
					ri *= 2
					<-time.After(time.Duration(ri) * time.Millisecond)
				} else {
					break
				}
			}
		}
	})
}

func retryProxyHandler(cl *http.Client, re *url.URL) http.Handler {
	pr := httputil.NewSingleHostReverseProxy(re)
	mt := NewMyTransport()
	pr.Transport = mt

	return retryHandler(cl, re, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Host = re.Host
		pr.ServeHTTP(w, r)
	}))

}

func (s *Web) Serve() error {
	addr := fmt.Sprintf("%s:%d", s.host, s.port)
	ln, err := net.Listen("tcp", addr)
	s.ln = ln
	if err != nil {
		return errors.Wrap(err, "failed to web listen to tcp connection")
	}
	re, err := url.Parse(s.upstream)
	if err != nil {
		return errors.Wrapf(err, "failed to parse remote")
	}
	tr := &http.Transport{
		Dial: (&net.Dialer{
			Timeout: 5 * time.Second,
		}).Dial,
	}
	cl := &http.Client{
		Timeout:   time.Second * 10,
		Transport: tr,
	}

	log.Infof("serving Web at %v", addr)
	srv := &http.Server{
		Handler:        retryProxyHandler(cl, re),
		MaxHeaderBytes: 50 << 20,
	}
	return srv.Serve(ln)
}

func (s *Web) Close() {
	log.Info("closing Web")
	defer func() {
		log.Info("Web closed")
	}()
	if s.ln != nil {
		s.ln.Close()
	}
}
