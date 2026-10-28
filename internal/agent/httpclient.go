package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"

	"lsl-wardline/internal/gateway"
)

// HTTPControl talks to the control plane's /agent/v1 endpoints.
type HTTPControl struct {
	base string
	hc   *http.Client
}

// NewHTTPControl returns a Control backed by HTTP. The client must not set a short overall
// timeout: Desired holds the request open.
func NewHTTPControl(baseURL string, hc *http.Client) *HTTPControl {
	if hc == nil {
		hc = &http.Client{}
	}
	return &HTTPControl{base: strings.TrimRight(baseURL, "/"), hc: hc}
}

func (h *HTTPControl) do(ctx context.Context, method, path, auth string, body any) (*http.Response, error) {
	var rd io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, h.base+path, rd)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	res, err := h.hc.Do(req)
	if err != nil {
		return nil, err
	}
	if res.StatusCode >= 400 {
		defer res.Body.Close()
		if res.StatusCode == http.StatusUnauthorized && auth != "" {
			return nil, ErrRevoked
		}
		var e struct {
			Error string `json:"error"`
		}
		json.NewDecoder(io.LimitReader(res.Body, 4096)).Decode(&e)
		if e.Error == "" {
			e.Error = "the control plane refused the request"
		}
		return nil, fmt.Errorf("%s", e.Error)
	}
	return res, nil
}

func (h *HTTPControl) Enroll(ctx context.Context, r EnrollRequest) (EnrollResponse, error) {
	res, err := h.do(ctx, http.MethodPost, "/agent/v1/enroll", "", r)
	if err != nil {
		return EnrollResponse{}, err
	}
	defer res.Body.Close()
	var out EnrollResponse
	return out, json.NewDecoder(res.Body).Decode(&out)
}

func (h *HTTPControl) Desired(ctx context.Context, auth string, since int64) (gateway.Signed, bool, error) {
	res, err := h.do(ctx, http.MethodGet, "/agent/v1/desired?since="+strconv.FormatInt(since, 10), auth, nil)
	if err != nil {
		return gateway.Signed{}, false, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNoContent {
		return gateway.Signed{}, false, nil
	}
	var s gateway.Signed
	if err := json.NewDecoder(res.Body).Decode(&s); err != nil {
		return gateway.Signed{}, false, err
	}
	return s, true, nil
}

func (h *HTTPControl) Report(ctx context.Context, auth string, r Report) error {
	res, err := h.do(ctx, http.MethodPost, "/agent/v1/report", auth, r)
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func (h *HTTPControl) Refresh(ctx context.Context, auth string, newPublicKey string) error {
	res, err := h.do(ctx, http.MethodPost, "/agent/v1/refresh", auth, RefreshRequest{PublicKey: newPublicKey})
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}

func (h *HTTPControl) Rollback(ctx context.Context, auth string) error {
	res, err := h.do(ctx, http.MethodPost, "/agent/v1/refresh/rollback", auth, struct{}{})
	if err != nil {
		return err
	}
	res.Body.Close()
	return nil
}
