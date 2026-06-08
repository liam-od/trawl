// Package job parses and resolves a one-shot transfer described as JSON on the
// command line, so a download (or upload) can be driven without the TUI. The
// spec names a saved host and a direction; everything else falls back to that
// host's configured default directories, mirroring the panels the TUI would
// have opened. Path resolution here is pure (no filesystem or network) so it can
// be unit-tested; the caller wires the result to the real filesystems.
package job

import (
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"
)

// Direction values for Spec.Type.
const (
	RemoteToLocal = "remote_to_local"
	LocalToRemote = "local_to_remote"
)

// Spec is the JSON payload passed to --transfer. Name selects a saved host and
// Type the direction; both are required. Object names the file or directory to
// move, relative to the base directory on the source side. RemotePath and
// LocalPath override the host's configured remote_dir/local_dir base directories
// for this transfer. An empty Object transfers the whole base directory.
type Spec struct {
	Name       string `json:"name"`
	Type       string `json:"type"`
	Object     string `json:"object,omitempty"`
	RemotePath string `json:"remote_path,omitempty"`
	LocalPath  string `json:"local_path,omitempty"`
}

// Parse decodes s into a Spec and validates it. Unknown fields are rejected so a
// typo'd key is a loud error rather than a silently ignored instruction. Object,
// if given, must stay within the base directory: no leading slash and no ".."
// segment.
func Parse(s string) (Spec, error) {
	dec := json.NewDecoder(strings.NewReader(s))
	dec.DisallowUnknownFields()

	var spec Spec
	if err := dec.Decode(&spec); err != nil {
		return Spec{}, fmt.Errorf("parse transfer spec: %w", err)
	}
	if dec.More() {
		return Spec{}, fmt.Errorf("parse transfer spec: trailing data after JSON object")
	}

	if spec.Name == "" {
		return Spec{}, fmt.Errorf("transfer spec: \"name\" is required")
	}
	switch spec.Type {
	case RemoteToLocal, LocalToRemote:
	case "":
		return Spec{}, fmt.Errorf("transfer spec: \"type\" is required (%s or %s)", RemoteToLocal, LocalToRemote)
	default:
		return Spec{}, fmt.Errorf("transfer spec: unknown type %q (want %s or %s)", spec.Type, RemoteToLocal, LocalToRemote)
	}
	if err := validateObject(spec.Object); err != nil {
		return Spec{}, err
	}
	return spec, nil
}

// validateObject rejects an object path that would escape the base directory.
// Objects are always slash-separated (they come from JSON), so the check is in
// terms of forward-slash segments.
func validateObject(object string) error {
	if object == "" {
		return nil
	}
	if strings.HasPrefix(object, "/") {
		return fmt.Errorf("transfer spec: \"object\" must be relative, not %q", object)
	}
	for _, seg := range strings.Split(object, "/") {
		if seg == ".." {
			return fmt.Errorf("transfer spec: \"object\" must not contain %q", "..")
		}
	}
	return nil
}

// Resolve computes the remote and local paths for the transfer from the resolved
// base directories. remoteBase and localBase are the base directories already
// expanded for their own machine (a remote ~ against the server home, a local ~
// against the local home); RemotePath/LocalPath overrides are applied by the
// caller before calling Resolve. srcIsRemote reports the direction: true means
// remotePath is the source and localPath the destination (a download).
//
// The source path is the base directory itself when Object is empty, otherwise
// base joined with Object. The destination is the matching base joined with the
// source's final element, so a download of "a/b.mkv" lands as "<localBase>/b.mkv".
func (s Spec) Resolve(remoteBase, localBase string) (remotePath, localPath string, srcIsRemote bool) {
	switch s.Type {
	case LocalToRemote:
		localPath = localSource(localBase, s.Object)
		remotePath = path.Join(remoteBase, baseName(s.Object, localBase))
		return remotePath, localPath, false
	default: // RemoteToLocal
		remotePath = remoteSource(remoteBase, s.Object)
		localPath = filepath.Join(localBase, baseName(s.Object, remoteBase))
		return remotePath, localPath, true
	}
}

// remoteSource is the POSIX source path for a remote object, or the base itself
// when object is empty.
func remoteSource(base, object string) string {
	if object == "" {
		return base
	}
	return path.Join(base, object)
}

// localSource is the native source path for a local object, or the base itself
// when object is empty. Object is slash-separated, so it is converted first.
func localSource(base, object string) string {
	if object == "" {
		return base
	}
	return filepath.Join(base, filepath.FromSlash(object))
}

// baseName returns the final element used for the destination name: the last
// segment of object, or — when object is empty — the last element of base (whose
// whole contents are being transferred). Object is always slash-separated.
func baseName(object, base string) string {
	if object != "" {
		return path.Base(object)
	}
	return path.Base(filepath.ToSlash(base))
}
