// Copyright 2018 Google LLC All Rights Reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package commands

import (
	"context"
	"fmt"
	gb "go/build"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/ko/pkg/build"
	"github.com/google/ko/pkg/publish"

	"golang.org/x/tools/go/packages"
)

func qualifyLocalImports(importpath string) ([]string, error) {
	cfg := &packages.Config{
		Mode: packages.NeedName,
	}
	pkgs, err := packages.Load(cfg, importpath)
	if err != nil {
		return nil, err
	}
	var paths []string
	for _, pkg := range pkgs {
		paths = append(paths, pkg.PkgPath)
	}
	return paths, nil
}

func publishImages(ctx context.Context, importpaths []string, pub publish.Interface, b build.Interface) (map[string]name.Reference, error) {
	imgs := make(map[string]name.Reference)

	// Local importpaths might be references to multiple paths (e.g.,
	// "./cmd/..."), so we need to resolve them to only those which are
	// supported by ko. Non-local importpaths don't need to be resolved.
	resolved := map[string]struct{}{}
	for _, importpath := range importpaths {
		if gb.IsLocalImport(importpath) {
			resolvedPaths, err := qualifyLocalImports(importpath)
			if err != nil {
				return nil, err
			}
			// If the requested importpath was an explicit single
			// package, and it's not supported, fail.
			if !strings.HasSuffix(importpath, "/...") {
				if err := b.IsSupportedReference(build.StrictScheme + resolvedPaths[0]); err != nil {
					return nil, fmt.Errorf("importpath %q is not supported: %v", resolvedPaths[0], err)
				}
				resolved[resolvedPaths[0]] = struct{}{}
			} else {
				// If a local importpath references multiple
				// packages ("./..."), add any that are
				// supported references. If none are supported,
				// then fail.
				var batch []string
				for _, rp := range resolvedPaths {
					if err := b.IsSupportedReference(build.StrictScheme + rp); err == nil {
						batch = append(batch, build.StrictScheme+rp)
					}
				}
				if len(batch) == 0 {
					return nil, fmt.Errorf("importpath %q references no supported paths", importpath)
				}
				for _, b := range batch {
					resolved[b] = struct{}{}
				}
			}
		} else {
			resolved[importpath] = struct{}{}
		}
	}

	for importpath := range resolved {
		img, err := b.Build(ctx, importpath)
		if err != nil {
			return nil, fmt.Errorf("error building %q: %v", importpath, err)
		}
		ref, err := pub.Publish(img, importpath)
		if err != nil {
			return nil, fmt.Errorf("error publishing %s: %v", importpath, err)
		}
		imgs[importpath] = ref
	}
	return imgs, nil
}
