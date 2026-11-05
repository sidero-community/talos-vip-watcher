// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package main

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestHTTPChecker(t *testing.T) {
	t.Parallel()

	for name, tc := range map[string]struct {
		status int
		want   string
	}{
		"ready":        {status: http.StatusOK},
		"not ready":    {status: http.StatusInternalServerError, want: "500 Internal Server Error"},
		"unauthorized": {status: http.StatusUnauthorized, want: "anonymous access to the health endpoint is not allowed"},
		"forbidden":    {status: http.StatusForbidden, want: "anonymous access to the health endpoint is not allowed"},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(srv.Close)

			// The test server's certificate is self-signed, like the API server's is to the container.
			err := newHTTPChecker(srv.URL+"/readyz", time.Second, true).Check(context.Background())

			switch {
			case tc.want == "" && err != nil:
				t.Fatalf("got %v, want healthy", err)
			case tc.want != "" && (err == nil || !strings.Contains(err.Error(), tc.want)):
				t.Fatalf("got %v, want an error containing %q", err, tc.want)
			}
		})
	}
}

func TestHTTPCheckerVerifiesTLSWhenAsked(t *testing.T) {
	t.Parallel()

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	t.Cleanup(srv.Close)

	if err := newHTTPChecker(srv.URL, time.Second, false).Check(context.Background()); err == nil {
		t.Fatal("a self-signed certificate passed verification")
	}
}

func TestHTTPCheckerUnreachable(t *testing.T) {
	t.Parallel()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {}))
	url := srv.URL
	srv.Close()

	if err := newHTTPChecker(url, time.Second, true).Check(context.Background()); err == nil {
		t.Fatal("an unreachable URL passed")
	}
}
