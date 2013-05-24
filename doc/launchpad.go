// Copyright (c) 2013 GPMGo Members. All rights reserved.
// Use of this source code is governed by a MIT-style
// license that can be found in the LICENSE file.

package doc

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"

	"github.com/GPMGo/gpm/utils"
)

var LaunchpadPattern = regexp.MustCompile(`^launchpad\.net/(?P<repo>(?P<project>[a-z0-9A-Z_.\-]+)(?P<series>/[a-z0-9A-Z_.\-]+)?|~[a-z0-9A-Z_.\-]+/(\+junk|[a-z0-9A-Z_.\-]+)/[a-z0-9A-Z_.\-]+)(?P<dir>/[a-z0-9A-Z_.\-/]+)*$`)

// GetLaunchpadDoc downloads tarball from launchpad.net.
func GetLaunchpadDoc(client *http.Client, match map[string]string, installGOPATH string, node *Node, cmdFlags map[string]bool) ([]string, error) {

	if match["project"] != "" && match["series"] != "" {
		rc, err := httpGet(client, expand("https://code.launchpad.net/{project}{series}/.bzr/branch-format", match), nil)
		switch {
		case err == nil:
			rc.Close()
			// The structure of the import path is launchpad.net/{root}/{dir}.
		case isNotFound(err):
			// The structure of the import path is is launchpad.net/{project}/{dir}.
			match["repo"] = match["project"]
			match["dir"] = expand("{series}{dir}", match)
		default:
			return nil, err
		}
	}

	// bundle and snapshot will have commit 'B' and 'S',
	// but does not need to download dependencies.
	isCheckImport := len(node.Value) == 0

	var downloadPath string
	// Check if download with specific revision.
	if isCheckImport || len(node.Value) == 1 {
		downloadPath = expand("https://bazaar.launchpad.net/+branch/{repo}/tarball", match)
		node.Type = "commit"
	} else {
		downloadPath = expand("https://bazaar.launchpad.net/+branch/{repo}/tarball/"+node.Value, match)
	}

	// Scrape the repo browser to find the project revision and individual Go files.
	p, err := HttpGetBytes(client, downloadPath, nil)
	if err != nil {
		return nil, err
	}

	projectPath := expand("launchpad.net/{repo}", match)
	installPath := installGOPATH + "/src/" + projectPath
	node.ImportPath = projectPath

	// Remove old files.
	os.RemoveAll(installPath + "/")
	// Create destination directory.
	os.MkdirAll(installPath+"/", os.ModePerm)

	gzr, err := gzip.NewReader(bytes.NewReader(p))
	if err != nil {
		return nil, err
	}
	defer gzr.Close()

	tr := tar.NewReader(gzr)

	isCodeOnly := cmdFlags["-c"]
	var autoPath string // Auto path is the root path that generated by bitbucket.org.
	// Get source file data.
	dirs := make([]string, 0, 5)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		} else if err != nil {
			return nil, err
		}

		fn := h.FileInfo().Name()
		// Check root path.
		if len(autoPath) == 0 {
			autoPath = fn[:strings.Index(fn, match["repo"])+len(match["repo"])]
		}
		absPath := strings.Replace(fn, autoPath, installPath, 1)

		switch {
		case h.FileInfo().IsDir(): // Directory.
			// Check if current directory is example.
			if !(!cmdFlags["-e"] && strings.Contains(absPath, "example")) {
				dirs = append(dirs, absPath)
			}
		case isCodeOnly && !utils.IsDocFile(path.Base(absPath)):
			continue
		case !strings.HasPrefix(fn, "."):
			// Create diretory before create file.
			os.MkdirAll(path.Dir(absPath)+"/", os.ModePerm)

			// Get data from archive.
			fbytes := make([]byte, h.Size)
			if _, err := io.ReadFull(tr, fbytes); err != nil {
				return nil, err
			}

			// Write data to file
			fw, err := os.Create(absPath)
			if err != nil {
				return nil, err
			}

			_, err = fw.Write(fbytes)
			fw.Close()
			if err != nil {
				return nil, err
			}
		}
	}

	var imports []string

	// Check if need to check imports.
	if isCheckImport {
		for _, d := range dirs {
			importPkgs, err := CheckImports(d+"/", match["importPath"])
			if err != nil {
				return nil, err
			}
			imports = append(imports, importPkgs...)
		}
	}

	return imports, err
}
