/*
Copyright 2015 Google Inc. All rights reserved.
Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at
    http://www.apache.org/licenses/LICENSE-2.0
Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package main

import (
	"bytes"
	"fmt"
	"go/build"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

func ensureVendor() {
	var buf bytes.Buffer
	cmd := exec.Command("vendor")
	cmd.Stderr = &buf
	cmd.Run()
	if !strings.HasPrefix(buf.String(), "Usage: vendor") {
		fmt.Fprintln(os.Stderr, "The save/restore functionality uses 'vendor'.")
		fmt.Fprintln(os.Stderr, "To install vendor, 'go get github.com/skelterjohn/vendor'.")
		os.Exit(1)
	}
	cmd = exec.Command("vendor", "-v")
	buf = bytes.Buffer{}
	cmd.Stdout = &buf
	cmd.Run()
	if !strings.HasPrefix(buf.String(), "vendor build ") {
		fmt.Fprintln(os.Stderr, "Your copy of vendor is out of date.")
		fmt.Fprintln(os.Stderr, "To update vendor, 'go get -u github.com/skelterjohn/vendor'.")
		os.Exit(1)
	}
	tokens := strings.Split(buf.String(), " ")
	numToken := strings.TrimSpace(tokens[2])
	if buildNum, err := strconv.Atoi(numToken); err != nil || buildNum < 3 {
		fmt.Fprintln(os.Stderr, "Your copy of vendor is out of date.")
		fmt.Fprintln(os.Stderr, "To update vendor, 'go get -u github.com/skelterjohn/vendor'.")
		os.Exit(1)
	}
}

func (w *workspace) getOutsidePackages(targets []string) map[string]string {
	os.Setenv("GOPATH", w.gopath())

	for _, gopath := range w.gopaths {
		target := "./" + gopath + "/src/..." // filepath.Join() doesn't like a leading dot.
		targets = append(targets, target)
	}

	goListTestArgs := []string{"list", "-e", "-f", "{{range .TestImports}}{{.}}\n{{end}}"}
	goListTestArgs = append(goListTestArgs, targets...)
	// fmt.Printf("%q\n", goListTestArgs)
	var testBuf bytes.Buffer
	cmd := exec.Command("go", goListTestArgs...)
	cmd.Dir = w.root
	cmd.Stdout = &testBuf
	orExit(cmd.Run())
	for _, pkg := range strings.Split(testBuf.String(), "\n") {
		targets = append(targets, pkg)
	}

	goListArgs := []string{"list", "-e", "-f", "{{.ImportPath}}\n{{range .Deps}}{{.}}\n{{end}}"}
	goListArgs = append(goListArgs, targets...)
	// fmt.Printf("%q\n", goListArgs)
	var buf bytes.Buffer
	cmd = exec.Command("go", goListArgs...)
	cmd.Dir = w.root
	cmd.Stdout = &buf
	orExit(cmd.Run())

	goroot := runtime.GOROOT()
	build.Default.GOPATH = w.gopath()

	pkgs := map[string]string{}
	for _, pkg := range strings.Split(buf.String(), "\n") {
		if pkg == "" {
			continue
		}
		p, err := build.Import(pkg, w.root, build.FindOnly)
		if err != nil {
			continue
		}
		if x, err := filepath.Rel(goroot, p.Dir); err == nil && !strings.HasPrefix(x, "..") {
			continue
		}
		pkgs[pkg] = p.Dir
	}

	for pkg, dir := range pkgs {
		if !filepath.IsAbs(dir) {
			continue
		}
		if x, err := filepath.Rel(w.root, dir); err == nil && !strings.HasPrefix(x, "..") {
			continue
		}
		pkgs[pkg] = dir
	}

	return pkgs
}

func save(w *workspace, args []string) {
	ensureVendor()

	var targets []string
	godeps := false
	for _, t := range args {
		if t == "--godeps" {
			godeps = true
		} else {
			targets = append(targets, t)
		}
	}

	pkgs := w.getOutsidePackages(targets)

	addonMapping := map[string]string{}
	for pkg, dir := range pkgs {
		destination := filepath.Join(w.vendorRootSrc(), pkg)
		// if it's already in here, vendor will pick it up
		if !filepath.IsAbs(dir) {
			continue
		}
		if x, err := filepath.Rel(w.root, dir); err == nil && !strings.HasPrefix(x, "..") {
			continue
		}
		addonMapping[destination] = dir
	}

	var addonArgs []string
	for destination, dir := range addonMapping {
		addonArgs = append(addonArgs, "-a", destination+"="+dir)
	}

	if godeps {
		for _, dd := range w.importGodeps() {
			rarg := dd.root + "=" + dd.repo + "@" + dd.rev
			addonArgs = append(addonArgs, "-r"+dd.kind, rarg)
		}
	}

	ignoreDirs := []string{".git", ".hg", ".gocfg"}
	for _, gopath := range w.gopaths {
		ignoreDirs = append(ignoreDirs,
			filepath.Join(gopath, "pkg"),
			filepath.Join(gopath, "bin"))
	}
	ignoreDirsStr := strings.Join(ignoreDirs, string(filepath.ListSeparator))
	os.Setenv("VENDOR_IGNORE_DIRS", ignoreDirsStr)

	w.shellOutToVendor(
		append([]string{"wgo", "vendor", "-x", "-s"}, addonArgs...))
}

func vendor(w *workspace, targets []string) {
	pkgs := w.getOutsidePackages(targets)

	firstGopath := "."
	if len(w.gopaths) != 0 {
		firstGopath = w.gopaths[0]
	}

	for pkg, dir := range pkgs {
		destination := filepath.Join(firstGopath, "src", pkg)
		// if it's already in here, vendor will pick it up
		if !filepath.IsAbs(dir) {
			continue
		}
		if x, err := filepath.Rel(w.root, dir); err == nil && !strings.HasPrefix(x, "..") {
			continue
		}
		if _, err := os.Stat(destination); err == nil {
			continue
		}
		fmt.Println(pkg)
		copyDir(dir, destination)
	}
}

func restore(w *workspace) {
	ensureVendor()

	w.shellOutToVendor([]string{"wgo", "vendor", "-r"})
}
