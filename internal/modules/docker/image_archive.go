package docker

import (
	"archive/tar"
	"encoding/json"
	"fmt"
	"io"
	"path"
	"strings"
)

// ImageArchiveManifestSummary is the comparison-relevant subset of one
// manifest.json entry produced by docker image save.
type ImageArchiveManifestSummary struct {
	ImageID  string
	RepoTags []string
}

type imageArchiveManifestEntry struct {
	Config   string          `json:"Config"`
	RepoTags json.RawMessage `json:"RepoTags"`
}

// ReadImageArchiveManifest reads manifest.json without extracting archive
// members to disk.
func ReadImageArchiveManifest(reader io.Reader) ([]ImageArchiveManifestSummary, error) {
	tarReader := tar.NewReader(reader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return nil, fmt.Errorf("image archive does not contain manifest.json")
		}
		if err != nil {
			return nil, fmt.Errorf("failed to read image archive: %w", err)
		}
		if path.Clean(header.Name) != "manifest.json" {
			continue
		}

		var manifest []imageArchiveManifestEntry
		decoder := json.NewDecoder(io.LimitReader(tarReader, header.Size))
		if err := decoder.Decode(&manifest); err != nil {
			return nil, fmt.Errorf("failed to decode image archive manifest.json: %w", err)
		}
		var trailing json.RawMessage
		if err := decoder.Decode(&trailing); err != io.EOF {
			if err == nil {
				return nil, fmt.Errorf("failed to decode image archive manifest.json: unexpected trailing JSON value")
			}
			return nil, fmt.Errorf("failed to decode image archive manifest.json: %w", err)
		}
		if len(manifest) == 0 {
			return nil, fmt.Errorf("image archive manifest.json has no entries")
		}

		result := make([]ImageArchiveManifestSummary, 0, len(manifest))
		for index, entry := range manifest {
			if entry.Config == "" {
				return nil, fmt.Errorf("image archive manifest entry %d has no Config", index+1)
			}
			imageID := strings.TrimSuffix(entry.Config, path.Ext(entry.Config))
			imageID = strings.TrimPrefix(imageID, "blobs/sha256/")
			if imageID == "" {
				return nil, fmt.Errorf("image archive manifest entry %d has an invalid Config", index+1)
			}
			if entry.RepoTags == nil {
				return nil, fmt.Errorf("image archive manifest entry %d has no RepoTags", index+1)
			}
			var repoTags []string
			if err := json.Unmarshal(entry.RepoTags, &repoTags); err != nil {
				return nil, fmt.Errorf("image archive manifest entry %d has invalid RepoTags: %w", index+1, err)
			}
			result = append(result, ImageArchiveManifestSummary{ImageID: imageID, RepoTags: repoTags})
		}
		return result, nil
	}
}

// ArchiveImageID converts a manifest hash to the Engine API image-ID shape.
func ArchiveImageID(imageID string) string {
	if strings.HasPrefix(imageID, "sha256:") {
		return imageID
	}
	return "sha256:" + imageID
}

// ImageArchiveMatches reports whether an archive contains exactly the desired
// image IDs and single requested tags, regardless of manifest entry order.
func ImageArchiveMatches(manifest []ImageArchiveManifestSummary, desired map[string]string) bool {
	if len(manifest) != len(desired) {
		return false
	}
	remaining := make(map[string]string, len(desired))
	for name, imageID := range desired {
		remaining[name] = imageID
	}
	for _, entry := range manifest {
		if len(entry.RepoTags) != 1 {
			return false
		}
		name := entry.RepoTags[0]
		imageID, found := remaining[name]
		if !found || imageID != ArchiveImageID(entry.ImageID) {
			return false
		}
		delete(remaining, name)
	}
	return len(remaining) == 0
}
