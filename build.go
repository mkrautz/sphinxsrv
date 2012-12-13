// Copyright (c) 2012 The SphinxSrv Authors
// The use of this source code is goverened by a BSD-style
// license that can be found in the LICENSE-file.

package main

import (
	"io/ioutil"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
)

// buildReq represents a request to the branchBuilder
// for building a particular branch of the documentation
// repo.
type buildReq struct {
	Ref     string
	RepoURL string
	Commit  string
}

var (
	// buildInit ensures that initialization of the branchBuildReq
	// channel and launching of the branchBuilderLoop goroutine
	// only handles once.
	buildInit sync.Once
	// branchBuildReq is used by branchBuilderLoop to recieve incoming
	// build requests submitted by submitBuildRequest.
	branchBuildReq chan buildReq
)

// submitBuildRequest submits a build request to the branchBuilder.
func submitBuildRequest(url string, ref string, commit string) {
	buildInit.Do(func() {
		branchBuildReq = make(chan buildReq)
		go branchBuilderLoop()
	})
	branchBuildReq <- buildReq{Ref: ref, RepoURL: url, Commit: commit}
}

// branchBuilderLoop invokes the branchBuilder sequentially for incoming
// build requests.
func branchBuilderLoop() {
	for req := range branchBuildReq {
		branchBuilder(req)
		cleanupStaleBuildOutputs()
	}
}

// isNotEmpty returns true if err is of type ENOTEMPTY.
func isNotEmpty(err error) bool {
	linkErr, ok := err.(*os.LinkError)
	if ok {
		return linkErr.Err == syscall.ENOTEMPTY
	}
	return false
}

// branchBuilder builds a specific branch according to a buildReq.
func branchBuilder(req buildReq) {
	splitRef := strings.Split(req.Ref, "/")
	branchName := splitRef[len(splitRef)-1]

	log.Printf("Requested build of ref %v (branch %v)", req.Ref, branchName)

	// First, ensure that the repo exists
	if !dirExists(repoDir) {
		cmd := exec.Command("git", "clone", req.RepoURL, repoDir)
		buf, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("git clone error: %v", err)
			log.Printf("git clone output: %v", string(buf))
			return
		}
	}

	// Fetch the ref and merge
	cmd := exec.Command("git", "fetch", req.RepoURL, req.Ref)
	cmd.Dir = repoDir
	buf, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("git fetch error: %v", err)
		log.Printf("git fetch output: %v", string(buf))
		return
	}

	// Merge
	cmd = exec.Command("git", "merge", "FETCH_HEAD")
	cmd.Dir = repoDir
	buf, err = cmd.CombinedOutput()
	if err != nil {
		log.Printf("git merge error: %v", err)
		log.Printf("git merge output: %v", string(buf))
		return
	}

	// Prepare build in temp dir
	tempDir, err := ioutil.TempDir("", "sphinxsrv")
	if err != nil {
		log.Printf("unable to create temp dir: %v", err)
		return
	}
	defer os.RemoveAll(tempDir)

	// Clone into temp dir
	tempRepoPath := filepath.Join(tempDir, "repo")
	cmd = exec.Command("git", "clone", "-b", branchName, repoDir, tempRepoPath)
	buf, err = cmd.CombinedOutput()
	if err != nil {
		log.Printf("git clone error: %v", err)
		log.Printf("git clone output: %v", err)
		return
	}

	// Build it
	cmd = exec.Command("make", "html")
	cmd.Env = []string{"PATH=" + os.ExpandEnv("${PATH}") + ":" + filepath.Join(os.ExpandEnv("${HOME}"), "sphinxenv", "bin")}
	cmd.Dir = tempRepoPath
	buf, err = cmd.CombinedOutput()
	if err != nil {
		log.Printf("make html error: %v", err)
		log.Printf("make html output: %v", err)
		return
	}

	// Rename into buildoutput
	buildOutputPath := filepath.Join(buildOutputDir, branchName + "-" + req.Commit)
	err = os.Rename(filepath.Join(tempRepoPath, "_build", "html"), buildOutputPath)
	if err != nil && isNotEmpty(err) {
		// Skip if already exists. It's just a duplicate build.
		// Not possible by just pushing, but could happen if the 'Test Hook' on GitHub is invoked.
		log.Printf("Skipped deployment of newly-built branch %v (%v) - directory already exists in buildoutput", branchName, req.Commit)
		return
	} else if err != nil {
		log.Printf("unable to rename into buildoutput: %v", err)
		return
	}

	// Deploy
	err = os.Symlink(buildOutputPath, filepath.Join(branchesDir, branchName))
	if err != nil {
		log.Printf("symlink error: %v", err)
		return
	}

	log.Printf("Successfully deployed newly-built branch %v (%v)", branchName, req.Commit)
}

// activeBuildOuptuts collects a slice of active build outputs.
func activeBuildOutputs() ([]string, error) {
	// Read contents of branchesDir to determine which
	// build output the active branches are currently
	// pointing at.
	f, err := os.Open(branchesDir)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Fetch the list of active branches
	activeBranchNames, err := f.Readdirnames(-1)
	if err != nil {
		return nil, err
	}

	// Collect a list of active buildOutputs
	activeBuildOutputs := []string{}
	for _, name := range activeBranchNames {
		activeBranchPath := filepath.Join(branchesDir, name)
		buildOutput, err := os.Readlink(activeBranchPath)
		if err != nil {
			return nil, err
		}
		activeBuildOutputs = append(activeBuildOutputs, buildOutput)
	}

	return activeBuildOutputs, nil
}

// allBuildOutputs collects a slice of all build outputs.
func allBuildOutputs() ([]string, error) {
	// Read contents of buildOutputDir to get all dirs
	f, err := os.Open(buildOutputDir)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	// Fetch all dirnames
	allDirsRel, err := f.Readdirnames(-1)
	if err != nil {
		return nil, err
	}

	// Make them absolute
	allBuildOutputs := []string{}
	for _, name := range allDirsRel {
		allBuildOutputs = append(allBuildOutputs, filepath.Join(buildOutputDir, name))
	}
	return allBuildOutputs, nil
}

// cleanupStaleBuildOutputs walks through the buildoutputs in the buildOutputsDir
// and removes the ones that are no longer in use.
func cleanupStaleBuildOutputs() {
	log.Printf("Requested cleanup of stale build dirs")

	active, err := activeBuildOutputs()
	if err != nil {
		log.Printf("unable to get list of active outputs: %v", err)
		return
	}

	all, err := allBuildOutputs()
	if err != nil {
		log.Printf("unable to get list of all build outputs: %v", err)
		return
	}

	// Find candidates for deletion.
	toDelete := []string{}
	for _, buildOutput := range all {
		isActive := false
		for _, activeDir := range active {
			if activeDir == buildOutput {
				isActive = true
				break
			}
		}
		if !isActive {
			toDelete = append(toDelete, buildOutput)
		}
	}

	log.Printf("Deleting build dirs: %v", toDelete)

	// Get rid of them.
	for _, buildOutput := range toDelete {
		err := os.RemoveAll(buildOutput)
		if err != nil {
			log.Printf("unable to remove dir: %v", err)
			return
		}
	}

	log.Printf("Successfully cleaned up buildOutputDir")
}
