package publish

import (
	"context"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/google/ko/pkg/build"
)

// Interface abstracts different methods for publishing images.
type Interface interface {
	// Publish uploads the given build.Result to a registry incorporating the
	// provided string into the image's repository name.  Returns the digest
	// of the published image.
	Publish(context.Context, build.Result, string) (name.Reference, error)

	// Close exists for the tarball implementation so we can
	// do the whole thing in one write.
	Close() error
}
