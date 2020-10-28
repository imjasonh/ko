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

package publish

import (
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/google/go-containerregistry/pkg/crane"
	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/go-containerregistry/pkg/registry"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/mutate"
	"github.com/google/go-containerregistry/pkg/v1/random"
	"github.com/google/ko/pkg/build"
)

var (
	img, _ = random.Image(1024, 3)
	idx, _ = random.Index(1024, 3, 3)
)

func init() {
	idx = mutate.AppendManifests(idx, mutate.IndexAddendum{
		Add: img,
		Descriptor: v1.Descriptor{
			Platform: &v1.Platform{OS: "os", Architecture: "arch"},
		},
	})
}

func TestDefault(t *testing.T) {
	base := "blah"
	importpath := "github.com/Google/go-containerregistry/cmd/crane"
	expectedRepo := fmt.Sprintf("%s/%s", base, strings.ToLower(importpath))
	server := httptest.NewServer(registry.New())
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse(%v) = %v", server.URL, err)
	}
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:latest", u.Host, expectedRepo))
	if err != nil {
		t.Fatal("NewTag() =", err)
	}

	repoName := fmt.Sprintf("%s/%s", u.Host, base)
	def, err := NewDefault(repoName)
	if err != nil {
		t.Error("NewDefault() =", err)
	}

	for _, br := range []build.Result{img, idx} {
		if d, err := def.Publish(br, build.StrictScheme+importpath); err != nil {
			t.Error("Publish() =", err)
		} else if !strings.HasPrefix(d.String(), tag.Repository.String()) {
			t.Errorf("Publish() = %v, wanted prefix %v", d, tag.Repository)
		}
	}

	// Check that the image within the index was also tagged with :latest-$os-$arch.
	idig, err := img.Digest()
	if err != nil {
		t.Fatal("img.Digest:", err)
	}
	itag := fmt.Sprintf("%s/%s:latest-os-arch", u.Host, expectedRepo)
	got, err := crane.Digest(itag)
	if err != nil {
		t.Fatal("crane.Digest:", err)
	}
	if idig.String() != got {
		t.Errorf("tagging didn't work: %s != %s", idig, got)
	}
}

func md5Hash(s string) string {
	// md5 as hex.
	hasher := md5.New()
	hasher.Write([]byte(s))
	return hex.EncodeToString(hasher.Sum(nil))
}

func TestDefaultWithCustomNamer(t *testing.T) {
	base := "blah"
	importpath := "github.com/Google/go-containerregistry/cmd/crane"
	expectedRepo := fmt.Sprintf("%s/%s", base, strings.ToLower(importpath))

	server := httptest.NewServer(registry.New())
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse(%v) = %v", server.URL, err)
	}
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:latest", u.Host, expectedRepo))
	if err != nil {
		t.Fatal("NewTag() =", err)
	}

	repoName := fmt.Sprintf("%s/%s", u.Host, base)

	def, err := NewDefault(repoName, WithNamer(md5Hash))
	if err != nil {
		t.Error("NewDefault() =", err)
	}

	for _, br := range []build.Result{img, idx} {
		if d, err := def.Publish(br, build.StrictScheme+importpath); err != nil {
			t.Error("Publish() =", err)
		} else if !strings.HasPrefix(d.String(), repoName) {
			t.Errorf("Publish() = %v, wanted prefix %v", d, tag.Repository)
		} else if !strings.HasSuffix(d.Context().String(), md5Hash(strings.ToLower(importpath))) {
			t.Errorf("Publish() = %v, wanted suffix %v", d.Context(), md5Hash(importpath))
		}
	}
}

func TestDefaultWithTags(t *testing.T) {
	base := "blah"
	importpath := "github.com/Google/go-containerregistry/cmd/crane"
	expectedRepo := fmt.Sprintf("%s/%s", base, strings.ToLower(importpath))

	server := httptest.NewServer(registry.New())
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse(%v) = %v", server.URL, err)
	}
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:notLatest", u.Host, expectedRepo))
	if err != nil {
		t.Fatal("NewTag() =", err)
	}

	repoName := fmt.Sprintf("%s/%s", u.Host, base)

	def, err := NewDefault(repoName, WithTags([]string{"notLatest", "v1.2.3"}))
	if err != nil {
		t.Error("NewDefault() =", err)
	}

	for _, br := range []build.Result{img, idx} {
		if d, err := def.Publish(br, build.StrictScheme+importpath); err != nil {
			t.Error("Publish() =", err)
		} else if !strings.HasPrefix(d.String(), repoName) {
			t.Errorf("Publish() = %v, wanted prefix %v", d, tag.Repository)
		} else if !strings.HasSuffix(d.Context().String(), strings.ToLower(importpath)) {
			t.Errorf("Publish() = %v, wanted suffix %v", d.Context(), md5Hash(importpath))
		}

		otherTag := fmt.Sprintf("%s/%s:v1.2.3", u.Host, expectedRepo)

		first, err := crane.Digest(tag.String())
		if err != nil {
			t.Fatal(err)
		}
		second, err := crane.Digest(otherTag)
		if err != nil {
			t.Fatal(err)
		}

		if first != second {
			t.Errorf("tagging didn't work: %s != %s", second, first)
		}
	}

	// Check that the image within the index was also tagged with :$tag-$os-$arch.
	for _, tt := range []string{"notLatest", "v1.2.3"} {
		itag := fmt.Sprintf("%s/%s:%s-os-arch", u.Host, expectedRepo, tt)
		if _, err := crane.Digest(itag); err != nil {
			t.Fatal("crane.Digest:", err)
		}
	}
}

func TestDefaultWithReleaseTag(t *testing.T) {
	img, err := random.Image(1024, 1)
	if err != nil {
		t.Fatal("random.Image() =", err)
	}
	base := "blah"
	releaseTag := "v1.2.3"
	importpath := "github.com/Google/go-containerregistry/cmd/crane"
	expectedRepo := fmt.Sprintf("%s/%s", base, strings.ToLower(importpath))
	headPathPrefix := fmt.Sprintf("/v2/%s/blobs/", expectedRepo)
	initiatePath := fmt.Sprintf("/v2/%s/blobs/uploads/", expectedRepo)
	manifestPath := fmt.Sprintf("/v2/%s/manifests/", expectedRepo)

	createdTags := make(map[string]struct{})

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodHead && strings.HasPrefix(r.URL.Path, headPathPrefix) && r.URL.Path != initiatePath {
			http.Error(w, "NotFound", http.StatusNotFound)
			return
		}
		switch {
		case r.URL.Path == "/v2/":
			w.WriteHeader(http.StatusOK)
		case r.URL.Path == initiatePath:
			if r.Method != http.MethodPost {
				t.Errorf("Method; got %v, want %v", r.Method, http.MethodPost)
			}
			http.Error(w, "Mounted", http.StatusCreated)
		case strings.HasPrefix(r.URL.Path, manifestPath):
			if r.Method != http.MethodPut {
				t.Errorf("Method; got %v, want %v", r.Method, http.MethodPut)
			}

			createdTags[strings.TrimPrefix(r.URL.Path, manifestPath)] = struct{}{}

			http.Error(w, "Created", http.StatusCreated)
		default:
			t.Fatal("Unexpected path:", r.URL.Path)
		}
	}))
	defer server.Close()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("url.Parse(%v) = %v", server.URL, err)
	}
	tag, err := name.NewTag(fmt.Sprintf("%s/%s:notLatest", u.Host, expectedRepo))
	if err != nil {
		t.Fatal("NewTag() =", err)
	}

	repoName := fmt.Sprintf("%s/%s", u.Host, base)

	def, err := NewDefault(repoName, WithTags([]string{releaseTag}))
	if err != nil {
		t.Error("NewDefault() =", err)
	}
	if d, err := def.Publish(img, build.StrictScheme+importpath); err != nil {
		t.Error("Publish() =", err)
	} else if !strings.HasPrefix(d.String(), repoName) {
		t.Errorf("Publish() = %v, wanted prefix %v", d, tag.Repository)
	} else if !strings.HasSuffix(d.Context().String(), strings.ToLower(importpath)) {
		t.Errorf("Publish() = %v, wanted suffix %v", d.Context(), md5Hash(importpath))
	} else if !strings.Contains(d.String(), releaseTag) {
		t.Errorf("Publish() = %v, wanted tag included: %v", d.String(), releaseTag)
	}

	if _, ok := createdTags["v1.2.3"]; !ok {
		t.Error("Tag v1.2.3 was not created.")
	}
}
