package proxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"time"

	"github.com/sony/gobreaker"
)

type Options struct {
	Name            string
	Target          *url.URL
	Transport       http.RoundTripper
	DialTimeout     time.Duration
	ResponseTimeout time.Duration

	BreakerMaxRequests  uint32
	BreakerInterval     time.Duration
	BreakerTimeout      time.Duration
	BreakerFailureRatio float64
	BreakerMinRequests  uint32
}

type Proxy struct {
	name    string
	target  *url.URL
	rp      *httputil.ReverseProxy
	breaker *gobreaker.CircuitBreaker
}

func New(opts Options) *Proxy {
	transport := opts.Transport
	if transport == nil {
		transport = &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   opts.DialTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          200,
			MaxIdleConnsPerHost:   50,
			IdleConnTimeout:       90 * time.Second,
			TLSHandshakeTimeout:   5 * time.Second,
			ExpectContinueTimeout: 1 * time.Second,
			ResponseHeaderTimeout: opts.ResponseTimeout,
			ForceAttemptHTTP2:     true,
		}
	}

	rp := &httputil.ReverseProxy{
		Director: func(req *http.Request) {
			req.URL.Scheme = opts.Target.Scheme
			req.URL.Host = opts.Target.Host
			req.Host = opts.Target.Host
			req.Header.Set("X-Forwarded-Host", req.Host)
			req.Header.Set("X-Forwarded-Proto", schemeOf(req))
		},
		Transport:     transport,
		FlushInterval: 100 * time.Millisecond,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			// Настоящие ошибки обрабатываются уровнем выше (router) — здесь
			// только пишем 502 и уходим. Роутер добавит JSON-обёртку.
			http.Error(w, `{"error":"upstream unavailable"}`, http.StatusBadGateway)
		},
	}

	cb := gobreaker.NewCircuitBreaker(gobreaker.Settings{
		Name:        opts.Name,
		MaxRequests: opts.BreakerMaxRequests,
		Interval:    opts.BreakerInterval,
		Timeout:     opts.BreakerTimeout,
		ReadyToTrip: func(c gobreaker.Counts) bool {
			if c.Requests < opts.BreakerMinRequests {
				return false
			}
			ratio := float64(c.TotalFailures) / float64(c.Requests)
			return ratio >= opts.BreakerFailureRatio
		},
		IsSuccessful: func(err error) bool {
			// Считаем только сетевые ошибки и 5xx как "провал".
			// 4xx — это ответ бизнес-логики, не поломка сервиса.
			return err == nil
		},
	})

	return &Proxy{name: opts.Name, target: opts.Target, rp: rp, breaker: cb}
}

func (p *Proxy) Name() string     { return p.name }
func (p *Proxy) Target() *url.URL { return p.target }

// ServeHTTP прогоняет запрос через circuit-breaker и reverse-proxy.
func (p *Proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	_, err := p.breaker.Execute(func() (any, error) {
		// Считаем 5xx как ошибку — для этого перехватываем WriteHeader.
		rec := &statusRecorder{ResponseWriter: w, status: 0}
		p.rp.ServeHTTP(rec, r)
		if rec.status >= 500 {
			return nil, errors.New("upstream 5xx")
		}
		return nil, nil
	})
	if err != nil {
		if errors.Is(err, gobreaker.ErrOpenState) || errors.Is(err, gobreaker.ErrTooManyRequests) {
			// Breaker открыт — 503, клиент может ретраить.
			w.Header().Set("Retry-After", "10")
			http.Error(w, `{"error":"service temporarily unavailable"}`, http.StatusServiceUnavailable)
			return
		}
		// Прочие ошибки уже обработаны ErrorHandler'ом reverse-proxy.
	}
}

type statusRecorder struct {
	http.ResponseWriter
	status int
}

func (s *statusRecorder) WriteHeader(code int) {
	s.status = code
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	return s.ResponseWriter.Write(b)
}

func schemeOf(r *http.Request) string {
	if r.TLS != nil {
		return "https"
	}
	if p := r.Header.Get("X-Forwarded-Proto"); p != "" {
		return p
	}
	return "http"
}

var _ = context.Background
