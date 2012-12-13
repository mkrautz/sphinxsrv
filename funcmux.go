// Copyright (c) 2012 The SphinxSrv Authors
// The use of this source code is goverened by a BSD-style
// license that can be found in the LICENSE-file.

package main

import (
	"net/http"
)

// handleFunc represents a function that can be transformed into a
// http.Handler via a funcMux.
type handleFunc func(rw http.ResponseWriter, req *http.Request)

// funcMux translates a handleFunc into a http.Handler.
type funcMux struct {
	fn handleFunc
}

// ServeHTTP implments the http.Handler interface for funcMux.
func (fm funcMux) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	fm.fn(rw, req)
}
