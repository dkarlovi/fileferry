// Package mtp provides read/delete access to MTP (Media Transfer Protocol)
// devices such as Android phones, via the Windows Portable Devices (WPD) COM
// API. MTP devices are not mounted as filesystems (they have no drive letter),
// so they cannot be reached through the os package; they are object stores
// addressed through COM.
//
// The real implementation lives in wpd_windows.go and is only built on Windows.
// On every other platform Open returns ErrUnsupported. WSL counts as "other":
// the phone is owned by the Windows host's WPD driver and is not visible inside
// the WSL VM, so this must run as a native Windows build.
package mtp

import (
	"errors"
	"io"
	"time"
)

// ErrUnsupported is returned by Open on platforms without WPD support.
var ErrUnsupported = errors.New("MTP sources are only supported on native Windows (not WSL)")

// Object is a single file on an MTP device.
type Object interface {
	// Name is the file's base name, e.g. "PXL_20240101_120000.dng".
	Name() string
	// RelPath is the path relative to the scanned folder, using "/" separators.
	// For a flat folder this equals Name; with recursion it may include
	// subfolders, e.g. "sub/PXL_20240101.dng".
	RelPath() string
	// Size is the content length in bytes.
	Size() int64
	// ModTime is the object's last-modified time, or the zero time if unknown.
	ModTime() time.Time
	// Open streams the object's content. The stream is sequential (no seeking).
	Open() (io.ReadCloser, error)
	// Delete removes the object from the device.
	Delete() error
}

// Session is an open connection to a folder on an MTP device. It must be kept
// alive for as long as its Objects are used (their Open/Delete calls reuse the
// underlying device session). Call Close when done.
type Session interface {
	// List returns the file objects under the session's folder. When recurse is
	// true it descends into subfolders.
	List(recurse bool) ([]Object, error)
	// Close releases the device session.
	Close() error
}
