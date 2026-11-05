// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net/http"
	"time"
)

// httpChecker reports whether a URL answers HTTP 200.
type httpChecker struct {
	client *http.Client
	url    string
}

func newHTTPChecker(url string, timeout time.Duration, insecure bool) *httpChecker {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	// The default URL is the node's own API server on the loopback address, whose serving
	// certificate is not issued by a CA the container knows.
	transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: insecure} //nolint:gosec // opt-out flag, see above

	return &httpChecker{
		client: &http.Client{Timeout: timeout, Transport: transport},
		url:    url,
	}
}

// Check returns nil when the URL answers HTTP 200, and why it is not healthy otherwise.
func (c *httpChecker) Check(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url, nil)
	if err != nil {
		return err
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return err
	}

	defer resp.Body.Close() //nolint:errcheck

	// Read a bounded amount of the body so the connection can be reused.
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10))

	switch resp.StatusCode {
	case http.StatusOK:
		return nil
	case http.StatusUnauthorized, http.StatusForbidden:
		return fmt.Errorf("%s answered %s: anonymous access to the health endpoint is not allowed", c.url, resp.Status)
	default:
		return fmt.Errorf("%s answered %s", c.url, resp.Status)
	}
}
