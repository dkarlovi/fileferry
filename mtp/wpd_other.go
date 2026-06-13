//go:build !windows

package mtp

// Open always fails on non-Windows platforms (including WSL). MTP/WPD is a
// Windows-only COM stack; see the package doc comment.
func Open(device, path string) (Session, error) {
	return nil, ErrUnsupported
}
