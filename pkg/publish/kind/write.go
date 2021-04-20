// Copyright 2020 Google LLC All Rights Reserved.
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

package kind

import (
	"fmt"
	"io"
	"os"
	"sync"

	"golang.org/x/sync/errgroup"

	"github.com/google/go-containerregistry/pkg/name"
	v1 "github.com/google/go-containerregistry/pkg/v1"
	"github.com/google/go-containerregistry/pkg/v1/tarball"

	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cluster/nodes"
)

// Supported since kind 0.8.0 (https://github.com/kubernetes-sigs/kind/releases/tag/v0.8.0)
const clusterNameEnvKey = "KIND_CLUSTER_NAME"

// provider is an interface for kind providers to facilitate testing.
type provider interface {
	ListInternalNodes(name string) ([]nodes.Node, error)
}

// GetProvider is a variable so we can override in tests.
var GetProvider = func() provider {
	return cluster.NewProvider()
}

// Tag adds a tag to an already existent image.
func Tag(src, dest name.Tag) error {
	return onEachNode(func(n nodes.Node) error {
		cmd := n.Command("ctr", "--namespace=k8s.io", "images", "tag", "--force", src.String(), dest.String())
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to tag image: %w", err)
		}
		return nil
	})
}

// writeMu serializes image imports to kind, to avoid locking situations
// observed when publishing multiple images to KinD at the same time.
var writeMu sync.Mutex

// Write saves the image into the kind nodes as the given tag.
func Write(tag name.Tag, img v1.Image) error {
	return onEachNode(func(n nodes.Node) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		pr, pw := io.Pipe()

		grp := errgroup.Group{}
		grp.Go(func() error {
			return pw.CloseWithError(tarball.Write(tag, img, pw))
		})

		cmd := n.Command("ctr", "--namespace=k8s.io", "images", "import", "-").SetStdin(pr)
		if err := cmd.Run(); err != nil {
			return fmt.Errorf("failed to load image to node %q: %w", n, err)
		}

		if err := grp.Wait(); err != nil {
			return fmt.Errorf("failed to write intermediate tarball representation: %w", err)
		}

		return nil
	})
}

// onEachNode executes the given function on each node. Exits on first error.
func onEachNode(f func(nodes.Node) error) error {
	nodeList, err := getNodes()
	if err != nil {
		return err
	}

	for _, n := range nodeList {
		if err := f(n); err != nil {
			return err
		}
	}
	return nil
}

// getNodes gets all the nodes of the default cluster.
// Returns an error if none were found.
func getNodes() ([]nodes.Node, error) {
	provider := GetProvider()

	clusterName := os.Getenv(clusterNameEnvKey)
	if clusterName == "" {
		clusterName = cluster.DefaultName
	}

	nodeList, err := provider.ListInternalNodes(clusterName)
	if err != nil {
		return nil, err
	}
	if len(nodeList) == 0 {
		return nil, fmt.Errorf("no nodes found for cluster %q", cluster.DefaultName)
	}

	return nodeList, nil
}
