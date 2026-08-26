package match

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/alyshmahell/matchora/lib/config"
)

type httpClient struct {
	hc             *http.Client
	userAgent      string
	retries        int
	backoffMin     int
	backoffMax     int
	attemptTimeout time.Duration
}

func newHTTP(cfg config.Config) *httpClient {
	timeout := cfg.HTTPTimeout()
	retries := cfg.HTTP.Retries
	if retries <= 0 {
		retries = 3
	}
	attempt := cfg.ProviderTimeout()
	if attempt <= 0 {
		attempt = 10 * time.Second
	}
	br := cfg.HTTPBackoff()
	return &httpClient{
		hc:             &http.Client{Timeout: timeout},
		userAgent:      "matchora/" + cfg.Version + " (+https://github.com/alyshmahell/matchora)",
		retries:        retries,
		backoffMin:     br.MinExp,
		backoffMax:     br.MaxExp,
		attemptTimeout: attempt,
	}
}

func (c *httpClient) get(ctx context.Context, url string, pace func(context.Context) error) ([]byte, int, error) {
	return c.do(ctx, http.MethodGet, url, "application/json", nil, pace)
}

func (c *httpClient) post(ctx context.Context, url, contentType string, body io.Reader) ([]byte, int, error) {
	return c.do(ctx, http.MethodPost, url, contentType, body, nil)
}

func (c *httpClient) do(ctx context.Context, method, url, contentType string, body io.Reader, pace func(context.Context) error) ([]byte, int, error) {
	canRetry := method == http.MethodGet && body == nil
	var lastErr error
	var lastCode int
	for attempt := 0; attempt < c.retries; attempt++ {
		if pace != nil {
			if err := pace(ctx); err != nil {
				return nil, 0, err
			}
		}
		reqCtx := ctx
		cancel := func() {}
		if canRetry && c.attemptTimeout > 0 {
			reqCtx, cancel = context.WithTimeout(ctx, c.attemptTimeout)
		}
		req, err := http.NewRequestWithContext(reqCtx, method, url, body)
		if err != nil {
			cancel()
			return nil, 0, err
		}
		req.Header.Set("User-Agent", c.userAgent)
		if contentType != "" {
			req.Header.Set("Content-Type", contentType)
		}
		resp, err := c.hc.Do(req)
		if err != nil {
			cancel()
			if !canRetry || attempt == c.retries-1 {
				return nil, 0, err
			}
			lastErr = err
			if err := sleepBackoff(ctx, c.backoff(attempt), 0); err != nil {
				return nil, 0, err
			}
			continue
		}
		b, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		if err != nil {
			return nil, resp.StatusCode, err
		}
		if canRetry && retryableStatus(resp.StatusCode) && attempt < c.retries-1 {
			lastErr = fmt.Errorf("status %d from %s", resp.StatusCode, url)
			lastCode = resp.StatusCode
			if err := sleepBackoff(ctx, c.backoff(attempt), retryAfter(resp, c.backoffMax)); err != nil {
				return nil, resp.StatusCode, err
			}
			continue
		}
		return b, resp.StatusCode, nil
	}
	return nil, lastCode, lastErr
}

func (c *httpClient) backoff(attempt int) time.Duration {
	e := c.backoffMin + 1 + attempt
	if e > c.backoffMax {
		e = c.backoffMax
	}
	return config.JitterExp(e)
}

func retryableStatus(code int) bool {
	return code == http.StatusTooManyRequests ||
		code == http.StatusBadGateway ||
		code == http.StatusServiceUnavailable ||
		code == http.StatusGatewayTimeout
}

func retryAfter(resp *http.Response, maxExp int) time.Duration {
	if resp == nil {
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(resp.Header.Get("Retry-After")))
	if err != nil || n <= 0 {
		return 0
	}
	d := time.Duration(n) * time.Second
	if maxExp < 0 {
		maxExp = 0
	}
	if maxExp > 62 {
		maxExp = 62
	}
	capd := time.Duration(1<<maxExp) * time.Millisecond
	if d > capd {
		return capd
	}
	return d
}

func sleepBackoff(ctx context.Context, wait, after time.Duration) error {
	if after > 0 {
		wait = after
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(wait):
		return nil
	}
}
