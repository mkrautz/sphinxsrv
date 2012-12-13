// Copyright (c) 2012 The SphinxSrv Authors
// The use of this source code is goverened by a BSD-style
// license that can be found in the LICENSE-file.

package main

import (
	"io"
	"log"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var (
	// repoDir represents the directory in which the repository
	// this sphinxsrv instance works on is checked out into.
	repoDir string
	// branchesDir represents the directory from which built
	// branches of Sphinx documentation are served from.
	branchesDir string
	// buildOutputDir represents the directory in which HTML output
	// is stored in before it is atomically symlinked to branchesDir
	// for deployment.
	buildOutputDir string
)

// dirExists returns true if a directory exists at path.
func dirExists(path string) bool {
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	return fi.IsDir() || fi.Mode() == os.ModeSymlink
}

// defaultBranch returns the default http.Handler that must be served
// when a branch was not specified.
func defaultBranch() http.Handler {
	return http.FileServer(http.Dir(path.Join(branchesDir, "master")))
}

// getBranch returns the http.Handler that must be used to serve the branch
// given by branchName.
func getBranch(branchName string) http.Handler {
	return funcMux{fn: func(rw http.ResponseWriter, req *http.Request) {
		if req.URL.Path == "/"+branchName {
			http.Redirect(rw, req, "/"+branchName+"/", 302)
			return
		}

		handle := http.StripPrefix("/"+branchName+"/", http.FileServer(http.Dir(path.Join(branchesDir, branchName))))
		handle.ServeHTTP(rw, req)
	}}
}

// mux implements the sphinxsrv's default muxer
type mux struct{}

// ServeHTTP implements the branch picker handler for the sphinxsrv's default mux.
func (m mux) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	log.Printf("%v - %v %v", req.RemoteAddr, req.Method, req.URL.String())

	urlPath := req.URL.Path[1:]
	splitPath := strings.Split(urlPath, "/")

	branchName := ""
	if len(splitPath) > 0 {
		if len(splitPath[0]) > 0 {
			branchPath := path.Join(branchesDir, splitPath[0])
			if dirExists(branchPath) {
				branchName = splitPath[0]
			}
		}
	}

	handler := defaultBranch()
	if len(branchName) > 0 {
		handler = getBranch(branchName)
	}
	handler.ServeHTTP(rw, req)
}

func mkdirOrDie(path string) {
	err := os.Mkdir(path, 0700)
	if err != nil && !os.IsExist(err) {
		log.Fatalf("unable to mkdir: %v", err)
	}
}

func main() {
	sphinxSrvHome := filepath.Join(os.ExpandEnv("${HOME}"), ".sphinxsrv")
	mkdirOrDie(sphinxSrvHome)

	branchesDir = filepath.Join(sphinxSrvHome, "branches")
	mkdirOrDie(branchesDir)

	buildOutputDir = filepath.Join(sphinxSrvHome, "buildoutput")
	mkdirOrDie(buildOutputDir)

	repoDir = filepath.Join(sphinxSrvHome, "repo")
	mkdirOrDie(repoDir)
	
	logFilePath := filepath.Join(sphinxSrvHome, "daemon.log")
	f, err := os.OpenFile(logFilePath, os.O_APPEND|os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		log.Fatalf("sphinxsrv: unable to open log file: %v", err)
	}
	log.SetOutput(io.MultiWriter(f, os.Stderr))

	http.HandleFunc("/hook", githubHook)
	http.Handle("/", mux{})

	log.Printf("sphinxsrv ready for duty")
	log.Printf("about to listen on :9090")
	http.ListenAndServe(":9090", nil)
}
