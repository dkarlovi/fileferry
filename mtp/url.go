package mtp

import (
	"fmt"
	"strings"
)

// Scheme is the prefix that marks a source path as an MTP device URL.
const Scheme = "mtp://"

// ParseURL splits an MTP source URL into the device's friendly name (as shown
// in Windows Explorer, e.g. "Pixel 9 Pro") and the on-device folder path.
//
//	mtp://Pixel 9 Pro/Internal shared storage/DCIM/Camera
//	      ^--------^  ^---------------------------------^
//	       device                  path
//
// Friendly names and folder names may contain spaces, so this is plain prefix
// + first-segment splitting rather than net/url parsing (which would require
// percent-encoding). The returned path uses "/" separators and has no leading
// or trailing slash.
func ParseURL(raw string) (device, path string, err error) {
	if !strings.HasPrefix(raw, Scheme) {
		return "", "", fmt.Errorf("not an MTP URL (missing %q prefix): %q", Scheme, raw)
	}
	rest := strings.Trim(strings.TrimPrefix(raw, Scheme), "/")
	if rest == "" {
		return "", "", fmt.Errorf("MTP URL has no device name: %q", raw)
	}
	device, path, _ = strings.Cut(rest, "/")
	if device == "" {
		return "", "", fmt.Errorf("MTP URL has no device name: %q", raw)
	}
	if path == "" {
		return "", "", fmt.Errorf("MTP URL has no on-device path (expected mtp://device/folder/...): %q", raw)
	}
	return device, path, nil
}

// IsURL reports whether the given source path is an MTP device URL.
func IsURL(raw string) bool {
	return strings.HasPrefix(raw, Scheme)
}
