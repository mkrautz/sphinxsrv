// Copyright (c) 2012 The SphinxSrv Authors
// The use of this source code is goverened by a BSD-style
// license that can be found in the LICENSE-file.

package main

import (
	"bytes"
	"encoding/json"
	"log"
	"net/http"
)

// whitelist is the list of IPs that may send GitHub web hook requests.
var whitelist = []string{"207.97.227.253", "50.57.128.197", "108.171.174.178"}

// githubWebHook is a Go representation of GitHub's WebHook
// JSON request.  It only includes the fields we need to
// implment our automatic builder.
type githubWebHook struct {
	Ref  string     `json:"ref"`
	After string    `json:"after"`
	Repo githubRepo `json:"repository"`
}

// githubRepo represents the repository object
// in githubWebHook.
type githubRepo struct {
	URL string `json:"url"`
}

// githubHook is the http handler that implements handling of incoming
// GitHub hook calls.
func githubHook(rw http.ResponseWriter, req *http.Request) {
	if req.Method != "POST" {
		http.NotFoundHandler().ServeHTTP(rw, req)
		return
	}

	// We only get X-Forwarded-For from Varnish, which
	// means that TLS connections do not get it. Not much
	// of a loss, since GitHub's webhooks don't work with
	// SNI anyway.
	reqAddr := req.Header.Get("X-Forwarded-For")
	ok := false
	for _, whiteAddr := range whitelist {
		if reqAddr == whiteAddr {
			ok = true
			break
		}
	}
	if !ok {
		http.NotFoundHandler().ServeHTTP(rw, req)
		return
	}

	wh := githubWebHook{}
	payload := req.FormValue("payload")
	dec := json.NewDecoder(bytes.NewBufferString(payload))
	err := dec.Decode(&wh)
	if err != nil {
		log.Printf("githubHook error: %v", err)
		http.Error(rw, "internal server error", 500)
		return
	}

	if len(wh.Ref) == 0 || len(wh.Repo.URL) == 0 {
		return
	}

	submitBuildRequest(wh.Repo.URL, wh.Ref, wh.After)
}
