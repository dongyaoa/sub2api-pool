package poolupdate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"strings"
	"time"
)

func Handler(worker *Worker) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		respond := func(status int, value any) { w.WriteHeader(status); _ = json.NewEncoder(w).Encode(value) }
		if r.URL.RawQuery != "" {
			respond(http.StatusBadRequest, map[string]string{"error": "query parameters are not accepted"})
			return
		}
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/v1/status":
			respond(http.StatusOK, worker.Status())
		case r.Method == http.MethodPost && r.URL.Path == "/v1/update":
			if strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0]) != "application/json" {
				respond(http.StatusUnsupportedMediaType, map[string]string{"error": "application/json required"})
				return
			}
			var request UpdateRequest
			dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
			dec.DisallowUnknownFields()
			if err := dec.Decode(&request); err != nil {
				respond(http.StatusBadRequest, map[string]string{"error": "invalid update request"})
				return
			}
			var extra any
			if dec.Decode(&extra) != io.EOF {
				respond(http.StatusBadRequest, map[string]string{"error": "exactly one update request required"})
				return
			}
			job, err := worker.Start(r.Context(), request)
			if err != nil {
				status := http.StatusBadRequest
				if errors.Is(err, ErrBusy) || errors.Is(err, ErrTargetChanged) {
					status = http.StatusConflict
				}
				if errors.Is(err, ErrUnavailable) {
					status = http.StatusServiceUnavailable
				}
				respond(status, map[string]string{"error": err.Error()})
				return
			}
			respond(http.StatusAccepted, job)
		default:
			respond(http.StatusNotFound, map[string]string{"error": "unknown updater endpoint"})
		}
	})
}

type Client struct{ http *http.Client }

func NewClient(socketPath string) *Client {
	transport := &http.Transport{Proxy: nil, DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", socketPath)
	}}
	return &Client{http: &http.Client{Transport: transport, Timeout: 60 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}}
}
func (c *Client) Status(ctx context.Context) (*Status, error) {
	var status Status
	if err := c.do(ctx, http.MethodGet, "/v1/status", nil, &status); err != nil {
		return nil, err
	}
	return &status, nil
}
func (c *Client) Start(ctx context.Context, request UpdateRequest) (*Job, error) {
	var job Job
	if err := c.do(ctx, http.MethodPost, "/v1/update", request, &job); err != nil {
		return nil, err
	}
	return &job, nil
}
func (c *Client) do(ctx context.Context, method, path string, payload, out any) error {
	var body io.Reader
	if payload != nil {
		data, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		body = bytes.NewReader(data)
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://pool-updater"+path, body)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return errors.New("host image updater is unavailable")
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		if resp.StatusCode == http.StatusConflict {
			return ErrBusy
		}
		if resp.StatusCode == http.StatusServiceUnavailable {
			return ErrUnavailable
		}
		return errors.New("host image updater rejected the request")
	}
	dec := json.NewDecoder(io.LimitReader(resp.Body, 65537))
	if err := dec.Decode(out); err != nil {
		return errors.New("invalid host updater response")
	}
	return nil
}
