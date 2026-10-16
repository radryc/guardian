package cdkgen

import (
	"embed"
	"io/fs"
)

// The template catalog source of truth lives in the Strata AWS project at
// aws/templates/aws/v1. Run `go generate ./internal/cdkgen` after changing it
// to refresh the embedded copy.
//
//go:generate sh -c "rm -rf embed/templates && mkdir -p embed/templates && cp -R ../../../aws/templates/aws embed/templates/aws"
//go:embed all:embed/templates/aws/v1
var templatesFS embed.FS

// embeddedTemplatesRoot is the path inside templatesFS that contains
// catalog.yaml, _shared/ and the per-asset template directories.
const embeddedTemplatesRoot = "embed/templates/aws/v1"

func embeddedTemplates() fs.FS {
	sub, err := fs.Sub(templatesFS, embeddedTemplatesRoot)
	if err != nil {
		panic(err)
	}
	return sub
}
