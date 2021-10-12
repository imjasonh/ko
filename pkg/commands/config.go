/*
Copyright 2018 Google LLC All Rights Reserved.

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

package commands

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/google/go-containerregistry/pkg/authn"
	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/daemon"
	"github.com/google/go-containerregistry/pkg/v1/empty"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/remote"
	"github.com/google/go-containerregistry/pkg/v1/types"
	"github.com/google/ko/pkg/build"
	"github.com/google/ko/pkg/commands/options"
	"github.com/google/ko/pkg/publish"
	"github.com/spf13/viper"
	"golang.org/x/tools/go/packages"
)

const (
	// configDefaultBaseImage is the default base image if not specified in .ko.yaml.
	configDefaultBaseImage = "gcr.io/distroless/static:nonroot"
)

var (
	defaultBaseImage   string
	baseImageOverrides map[string]string
	buildConfigs       map[string]build.Config
)

// getBaseImage returns a function that determines the base image for a given import path.
// If the `bo.BaseImage` parameter is non-empty, it overrides base image configuration from `.ko.yaml`.
func getBaseImage(platform string, bo *options.BuildOptions) build.GetBase {
	return func(ctx context.Context, s string) (name.Reference, build.Result, error) {
		s = strings.TrimPrefix(s, build.StrictScheme)
		// Viper configuration file keys are case insensitive, and are
		// returned as all lowercase.  This means that import paths with
		// uppercase must be normalized for matching here, e.g.
		//    github.com/GoogleCloudPlatform/foo/cmd/bar
		// comes through as:
		//    github.com/googlecloudplatform/foo/cmd/bar
		baseImages, ok := baseImageOverrides[strings.ToLower(s)]
		if !ok {
			baseImages = defaultBaseImage
		}
		if bo.BaseImage != "" {
			baseImages = bo.BaseImage
		}
		nameOpts := []name.Option{}
		if bo.InsecureRegistry {
			nameOpts = append(nameOpts, name.Insecure)
		}
		var idxs []v1.ImageIndex
		var ref name.Reference
		for _, baseRef := range strings.Split(baseImages, ",") {
			var err error
			ref, err = name.ParseReference(baseRef, nameOpts...)
			if err != nil {
				return nil, nil, fmt.Errorf("parsing base image (%q): %v", baseRef, err)
			}
			// For ko.local, look in the daemon.
			if ref.Context().RegistryStr() == publish.LocalDomain {
				if strings.Contains(baseImages, ",") {
					return nil, nil, errors.New("cannot specify multiple base images when one uses the local domain")
				}
				img, err := daemon.Image(ref)
				return ref, img, err
			}

			userAgent := ua()
			if bo.UserAgent != "" {
				userAgent = bo.UserAgent
			}
			ropt := []remote.Option{
				remote.WithAuthFromKeychain(authn.DefaultKeychain),
				remote.WithUserAgent(userAgent),
				remote.WithContext(ctx),
			}

			// Using --platform=all will use an image index for the base,
			// otherwise we'll resolve it to the appropriate platform.
			//
			// Platforms can be comma-separated if we only want a subset of the base
			// image.
			multiplatform := platform == "all" || strings.Contains(platform, ",")
			var p v1.Platform
			if platform != "" && !multiplatform {
				parts := strings.Split(platform, "/")
				if len(parts) > 0 {
					p.OS = parts[0]
				}
				if len(parts) > 1 {
					p.Architecture = parts[1]
				}
				if len(parts) > 2 {
					p.Variant = parts[2]
				}
				if len(parts) > 3 {
					return nil, nil, fmt.Errorf("too many slashes in platform spec: %s", platform)
				}
				ropt = append(ropt, remote.WithPlatform(p))
			}

			log.Printf("Using base %s for %s", ref, s)
			desc, err := remote.Get(ref, ropt...)
			if err != nil {
				return nil, nil, err
			}
			switch desc.MediaType {
			case types.OCIImageIndex, types.DockerManifestList:
				if multiplatform {
					idx, err := desc.ImageIndex()
					if err != nil {
						return nil, nil, err
					}
					idxs = append(idxs, idx)
					continue
				}
				img, err := desc.Image()
				return ref, img, err
			default:
				if strings.Contains(baseImages, ",") {
					return nil, nil, errors.New("cannot specify multiple base images when one is a single-platform image")
				}
				img, err := desc.Image()
				return ref, img, err
			}
		}
		idx, err := combine(idxs)
		if len(idxs) != 1 {
			ref = nil
		}
		return ref, idx, err
	}
}

func combine(idxs []v1.ImageIndex) (v1.ImageIndex, error) {
	if len(idxs) == 1 {
		return idxs[0], nil
	}

	plats := map[string]bool{}
	var adds []mutate.IndexAddendum
	add := func(idx v1.ImageIndex) error {
		mf, err := idx.IndexManifest()
		if err != nil {
			return fmt.Errorf("IndexManifest: %w", err)
		}
		for _, desc := range mf.Manifests {
			if desc.Platform == nil {
				return fmt.Errorf("found nil platform for manifest %s", desc.Digest)
			}

			b, err := json.Marshal(desc.Platform)
			if err != nil {
				return fmt.Errorf("marshalling platform: %w", err)
			}
			if plats[string(b)] {
				return fmt.Errorf("conflicting platform %+v", *desc.Platform)
			}
			plats[string(b)] = true

			img, err := idx.Image(desc.Digest)
			if err != nil {
				return fmt.Errorf("getting image %s: %w", desc.Digest, err)
			}
			adds = append(adds, mutate.IndexAddendum{
				Add:        img,
				Descriptor: desc,
			})
		}
		return nil
	}
	for _, idx := range idxs {
		if err := add(idx); err != nil {
			return nil, err
		}
	}

	return mutate.AppendManifests(mutate.IndexMediaType(empty.Index, types.DockerManifestList), adds...), nil
}

func getTimeFromEnv(env string) (*v1.Time, error) {
	epoch := os.Getenv(env)
	if epoch == "" {
		return nil, nil
	}

	seconds, err := strconv.ParseInt(epoch, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("the environment variable %s should be the number of seconds since January 1st 1970, 00:00 UTC, got: %v", env, err)
	}
	return &v1.Time{Time: time.Unix(seconds, 0)}, nil
}

func getCreationTime() (*v1.Time, error) {
	return getTimeFromEnv("SOURCE_DATE_EPOCH")
}

func getKoDataCreationTime() (*v1.Time, error) {
	return getTimeFromEnv("KO_DATA_DATE_EPOCH")
}

func createCancellableContext() context.Context {
	signals := make(chan os.Signal)
	signal.Notify(signals, syscall.SIGINT, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		<-signals
		cancel()
	}()

	return ctx
}

func createBuildConfigMap(workingDirectory string, configs []build.Config) (map[string]build.Config, error) {
	buildConfigsByImportPath := make(map[string]build.Config)
	for i, config := range configs {
		// Make sure to behave like GoReleaser by defaulting to the current
		// directory in case the build or main field is not set, check
		// https://goreleaser.com/customization/build/ for details
		if config.Dir == "" {
			config.Dir = "."
		}
		if config.Main == "" {
			config.Main = "."
		}

		// baseDir is the directory where `go list` will be run to look for package information
		baseDir := filepath.Join(workingDirectory, config.Dir)

		// To behave like GoReleaser, check whether the configured `main` config value points to a
		// source file, and if so, just use the directory it is in
		path := config.Main
		if fi, err := os.Stat(filepath.Join(baseDir, config.Main)); err == nil && fi.Mode().IsRegular() {
			path = filepath.Dir(config.Main)
		}

		// By default, paths configured in the builds section are considered
		// local import paths, therefore add a "./" equivalent as a prefix to
		// the constructured import path
		localImportPath := fmt.Sprint(".", string(filepath.Separator), path)

		pkgs, err := packages.Load(&packages.Config{Mode: packages.NeedName, Dir: baseDir}, localImportPath)
		if err != nil {
			return nil, fmt.Errorf("'builds': entry #%d does not contain a valid local import path (%s) for directory (%s): %v", i, localImportPath, baseDir, err)
		}

		if len(pkgs) != 1 {
			return nil, fmt.Errorf("'builds': entry #%d results in %d local packages, only 1 is expected", i, len(pkgs))
		}
		importPath := pkgs[0].PkgPath
		buildConfigsByImportPath[importPath] = config
	}

	return buildConfigsByImportPath, nil
}

// loadConfig reads build configuration from defaults, environment variables, and the `.ko.yaml` config file.
func loadConfig(workingDirectory string) error {
	v := viper.New()
	if workingDirectory == "" {
		workingDirectory = "."
	}
	// If omitted, use this base image.
	v.SetDefault("defaultBaseImage", configDefaultBaseImage)
	v.SetConfigName(".ko") // .yaml is implicit
	v.SetEnvPrefix("KO")
	v.AutomaticEnv()

	if override := os.Getenv("KO_CONFIG_PATH"); override != "" {
		v.AddConfigPath(override)
	}

	v.AddConfigPath(workingDirectory)

	if err := v.ReadInConfig(); err != nil {
		if _, ok := err.(viper.ConfigFileNotFoundError); !ok {
			return fmt.Errorf("error reading config file: %v", err)
		}
	}

	refs := v.GetString("defaultBaseImage")
	for _, ref := range strings.Split(refs, ",") {
		if _, err := name.ParseReference(ref); err != nil {
			return fmt.Errorf("'defaultBaseImage': error parsing %q as image reference: %v", ref, err)
		}
	}
	defaultBaseImage = refs

	baseImageOverrides = make(map[string]string)
	overrides := v.GetStringMapString("baseImageOverrides")
	for key, values := range overrides {
		for _, value := range strings.Split(values, ",") {
			if _, err := name.ParseReference(value); err != nil {
				return fmt.Errorf("'baseImageOverrides': error parsing %q as image reference: %v", value, err)
			}
		}
		baseImageOverrides[key] = values
	}

	var builds []build.Config
	if err := v.UnmarshalKey("builds", &builds); err != nil {
		return fmt.Errorf("configuration section 'builds' cannot be parsed")
	}
	var err error
	buildConfigs, err = createBuildConfigMap(workingDirectory, builds)
	return err
}
