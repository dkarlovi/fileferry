//go:build windows

package mtp

import (
	"fmt"
	"io"
	"runtime"
	"strings"
	"syscall"
	"time"
	"unicode/utf16"
	"unsafe"

	ole "github.com/go-ole/go-ole"
)

// This file talks to the Windows Portable Devices (WPD) COM API directly.
// WPD interfaces are vtable COM (not IDispatch), so methods are invoked by index
// into the object's vtable via syscall. The index of each method below follows
// its declaration order in the WPD headers (the first three slots, 0..2, are
// always IUnknown's QueryInterface/AddRef/Release).
//
// All COM work runs on a single OS thread pinned by an executor (see executor),
// initialized as an MTA apartment. WPD calls must not hop threads, and entries'
// Open/Delete may be invoked from arbitrary goroutines, so every COM touch is
// marshaled onto that one thread.

// COM CLSIDs / IIDs we instantiate. Interfaces obtained as out-parameters
// (content, properties, resources, enumerators) don't need IIDs here.
var (
	clsidDeviceManager         = ole.NewGUID("{0AF10CEC-2ECD-4B92-9581-34F6AE0637F3}")
	iidDeviceManager           = ole.NewGUID("{A1567595-4C2F-4574-A6FA-ECEF917B9A40}")
	clsidDeviceFTM             = ole.NewGUID("{F7C0039A-4762-488A-B4B3-760EF9A1BA9B}")
	iidDevice                  = ole.NewGUID("{625E2DF8-6392-4CF0-9AD1-3CFA5F17775C}")
	clsidDeviceValues          = ole.NewGUID("{0C15D503-D017-47CE-9016-7B3F978721CC}")
	iidDeviceValues            = ole.NewGUID("{6848F6F2-3155-4F86-B6F5-263EEEAB3143}")
	clsidPropVariantCollection = ole.NewGUID("{08A99E2F-6D6D-4B80-AF5A-BAF2BCBE4CB9}")
	iidPropVariantCollection   = ole.NewGUID("{89B2E422-4F1B-4316-BCEF-A44AFEA83EB3}")
)

// PROPERTYKEY (GUID + DWORD). Layout matches the Win32 struct exactly.
type propertyKey struct {
	fmtid ole.GUID
	pid   uint32
}

// WPD object properties live under one well-known format GUID.
var fmtidObject = ole.NewGUID("{EF6B490D-5CD8-437A-AFFC-DA8B60EE4A3C}")

func objKey(pid uint32) propertyKey { return propertyKey{*fmtidObject, pid} }

var (
	keyObjectName       = objKey(4)
	keyContentType      = objKey(7)
	keyObjectSize       = objKey(11)
	keyOriginalFileName = objKey(12)
	keyResourceDefault  = propertyKey{*ole.NewGUID("{E81E79BE-34F0-41BF-B53F-F1A06AE87842}"), 0}
	keyClientName       = propertyKey{*ole.NewGUID("{204D9F0C-2292-4080-9F42-40664E70F859}"), 2}
)

// Content-type GUIDs that mark an object as a container to descend into.
var (
	contentTypeFolder           = ole.NewGUID("{27E2E392-A111-48E0-AB0C-E17705A05F85}")
	contentTypeFunctionalObject = ole.NewGUID("{99ED0160-17FF-4C44-9D98-1D7A6F941921}")
)

// deviceRootObjectID is the WPD root from which enumeration starts.
const deviceRootObjectID = "DEVICE"

// vtLPWSTR is the PROPVARIANT type tag for a wide-string pointer.
const vtLPWSTR = 31

const ptrSize = unsafe.Sizeof(uintptr(0))

// propVariant mirrors Win32 PROPVARIANT closely enough for the VT_LPWSTR values
// we construct (object IDs passed to Delete).
type propVariant struct {
	vt  uint16
	_   uint16
	_   uint16
	_   uint16
	val uintptr
	_   uintptr
}

// call invokes the COM method at vtable index on the interface pointer `this`.
func call(this uintptr, index int, args ...uintptr) uintptr {
	vtbl := *(*uintptr)(unsafe.Pointer(this))
	fn := *(*uintptr)(unsafe.Pointer(vtbl + uintptr(index)*ptrSize))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{this}, args...)...)
	return r
}

func failed(hr uintptr) bool { return int32(hr) < 0 }

// release calls IUnknown::Release (vtable index 2) on a non-null interface.
func release(this uintptr) {
	if this != 0 {
		call(this, 2)
	}
}

func utf16Ptr(s string) *uint16 {
	p, _ := syscall.UTF16PtrFromString(s)
	return p
}

func utf16PtrToString(p *uint16) string {
	if p == nil {
		return ""
	}
	var out []uint16
	for ptr := unsafe.Pointer(p); ; ptr = unsafe.Add(ptr, 2) {
		c := *(*uint16)(ptr)
		if c == 0 {
			break
		}
		out = append(out, c)
	}
	return string(utf16.Decode(out))
}

func guidEqual(a, b *ole.GUID) bool { return *a == *b }

// executor owns a single OS thread with COM initialized as MTA. All COM calls
// run through do().
type executor struct {
	jobs chan func()
	quit chan struct{}
}

func newExecutor() *executor {
	e := &executor{jobs: make(chan func()), quit: make(chan struct{})}
	ready := make(chan struct{})
	go func() {
		runtime.LockOSThread()
		defer runtime.UnlockOSThread()
		_ = ole.CoInitializeEx(0, ole.COINIT_MULTITHREADED)
		defer ole.CoUninitialize()
		close(ready)
		for {
			select {
			case fn := <-e.jobs:
				fn()
			case <-e.quit:
				return
			}
		}
	}()
	<-ready
	return e
}

func (e *executor) do(fn func()) {
	done := make(chan struct{})
	e.jobs <- func() {
		defer close(done)
		fn()
	}
	<-done
}

func (e *executor) stop() { close(e.quit) }

// wpdSession implements Session over an open WPD device folder.
type wpdSession struct {
	exec         *executor
	devicePtr    uintptr
	contentPtr   uintptr
	propsPtr     uintptr
	resourcesPtr uintptr
	rootObjectID string
}

// Open connects to the named device, navigates to the folder, and returns a
// live session. All setup runs on the executor thread.
func Open(device, path string) (Session, error) {
	e := newExecutor()
	s := &wpdSession{exec: e}
	var err error
	e.do(func() { err = s.connect(device, path) })
	if err != nil {
		e.do(func() { s.releaseAll() })
		e.stop()
		return nil, err
	}
	return s, nil
}

func (s *wpdSession) connect(device, path string) error {
	mgrUnk, err := ole.CreateInstance(clsidDeviceManager, iidDeviceManager)
	if err != nil {
		return fmt.Errorf("create WPD device manager (is Windows Portable Devices available?): %w", err)
	}
	mgr := uintptr(unsafe.Pointer(mgrUnk))
	defer release(mgr)

	pnpID, err := findDevice(mgr, device)
	if err != nil {
		return err
	}

	devUnk, err := ole.CreateInstance(clsidDeviceFTM, iidDevice)
	if err != nil {
		return fmt.Errorf("create WPD device: %w", err)
	}
	s.devicePtr = uintptr(unsafe.Pointer(devUnk))

	clientInfo, err := newClientInfo()
	if err != nil {
		return err
	}
	defer release(clientInfo)

	// IPortableDevice::Open(pszPnPDeviceID, pClientInfo) — index 3.
	if r := call(s.devicePtr, 3, uintptr(unsafe.Pointer(utf16Ptr(pnpID))), clientInfo); failed(r) {
		return fmt.Errorf("open device %q: %w", device, ole.NewError(r))
	}
	// IPortableDevice::Content(ppContent) — index 5.
	if r := call(s.devicePtr, 5, uintptr(unsafe.Pointer(&s.contentPtr))); failed(r) {
		return fmt.Errorf("get device content: %w", ole.NewError(r))
	}
	// IPortableDeviceContent::Properties(ppProperties) — index 4.
	if r := call(s.contentPtr, 4, uintptr(unsafe.Pointer(&s.propsPtr))); failed(r) {
		return fmt.Errorf("get device properties: %w", ole.NewError(r))
	}
	// IPortableDeviceContent::Transfer(ppResources) — index 5.
	if r := call(s.contentPtr, 5, uintptr(unsafe.Pointer(&s.resourcesPtr))); failed(r) {
		return fmt.Errorf("get device resources: %w", ole.NewError(r))
	}

	folderID, err := s.resolveFolder(path)
	if err != nil {
		return err
	}
	s.rootObjectID = folderID
	return nil
}

// newClientInfo builds the IPortableDeviceValues handed to Open. An empty
// collection is accepted; we set a client name for good measure.
func newClientInfo() (uintptr, error) {
	unk, err := ole.CreateInstance(clsidDeviceValues, iidDeviceValues)
	if err != nil {
		return 0, fmt.Errorf("create client info: %w", err)
	}
	p := uintptr(unsafe.Pointer(unk))
	// IPortableDeviceValues::SetStringValue(key, value) — index 7.
	call(p, 7, uintptr(unsafe.Pointer(&keyClientName)), uintptr(unsafe.Pointer(utf16Ptr("fileferry"))))
	return p, nil
}

// findDevice returns the PnP device ID whose friendly name matches `want`.
func findDevice(mgr uintptr, want string) (string, error) {
	// IPortableDeviceManager::GetDevices(pPnPDeviceIDs, pcPnPDeviceIDs) — index 3.
	var count uint32
	if r := call(mgr, 3, 0, uintptr(unsafe.Pointer(&count))); failed(r) {
		return "", fmt.Errorf("enumerate devices: %w", ole.NewError(r))
	}
	if count == 0 {
		return "", fmt.Errorf("no MTP devices connected (looking for %q)", want)
	}
	ids := make([]*uint16, count)
	if r := call(mgr, 3, uintptr(unsafe.Pointer(&ids[0])), uintptr(unsafe.Pointer(&count))); failed(r) {
		return "", fmt.Errorf("enumerate devices: %w", ole.NewError(r))
	}

	var names []string
	match := ""
	for _, idPtr := range ids[:count] {
		pnpID := utf16PtrToString(idPtr)
		name := deviceFriendlyName(mgr, idPtr)
		ole.CoTaskMemFree(uintptr(unsafe.Pointer(idPtr)))
		names = append(names, name)
		if match == "" && strings.EqualFold(strings.TrimSpace(name), strings.TrimSpace(want)) {
			match = pnpID
		}
	}
	if match == "" {
		return "", fmt.Errorf("MTP device %q not found; connected devices: %s", want, strings.Join(names, ", "))
	}
	return match, nil
}

func deviceFriendlyName(mgr uintptr, idPtr *uint16) string {
	// IPortableDeviceManager::GetDeviceFriendlyName(pszPnPDeviceID, pName, pcch) — index 5.
	var n uint32
	if r := call(mgr, 5, uintptr(unsafe.Pointer(idPtr)), 0, uintptr(unsafe.Pointer(&n))); failed(r) || n == 0 {
		return ""
	}
	buf := make([]uint16, n)
	if r := call(mgr, 5, uintptr(unsafe.Pointer(idPtr)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n))); failed(r) {
		return ""
	}
	return syscall.UTF16ToString(buf)
}

// childInfo is a single enumerated child object.
type childInfo struct {
	id       string
	name     string
	size     uint64
	isFolder bool
}

// resolveFolder walks the on-device path from the root, matching each segment
// against child object names, and returns the target folder's object ID.
func (s *wpdSession) resolveFolder(path string) (string, error) {
	current := deviceRootObjectID
	for _, seg := range strings.Split(path, "/") {
		if seg == "" {
			continue
		}
		children, err := s.children(current)
		if err != nil {
			return "", err
		}
		next := ""
		for _, c := range children {
			if strings.EqualFold(c.name, seg) {
				next = c.id
				break
			}
		}
		if next == "" {
			return "", fmt.Errorf("on-device folder %q not found", seg)
		}
		current = next
	}
	return current, nil
}

// children enumerates the immediate children of parentID, reading each child's
// name, size, and folder-ness. Must run on the executor thread.
func (s *wpdSession) children(parentID string) ([]childInfo, error) {
	// IPortableDeviceContent::EnumObjects(dwFlags, pszParentObjectID, pFilter, ppEnum) — index 3.
	var enumPtr uintptr
	if r := call(s.contentPtr, 3, 0, uintptr(unsafe.Pointer(utf16Ptr(parentID))), 0, uintptr(unsafe.Pointer(&enumPtr))); failed(r) {
		return nil, fmt.Errorf("enumerate %q: %w", parentID, ole.NewError(r))
	}
	defer release(enumPtr)

	var out []childInfo
	const batch = 32
	for {
		ids := make([]*uint16, batch)
		var fetched uint32
		// IEnumPortableDeviceObjectIDs::Next(cObjects, pObjIDs, pcFetched) — index 3.
		r := call(enumPtr, 3, batch, uintptr(unsafe.Pointer(&ids[0])), uintptr(unsafe.Pointer(&fetched)))
		if failed(r) {
			return nil, fmt.Errorf("enumerate next: %w", ole.NewError(r))
		}
		for i := 0; i < int(fetched); i++ {
			objID := utf16PtrToString(ids[i])
			ole.CoTaskMemFree(uintptr(unsafe.Pointer(ids[i])))
			name, size, isFolder, err := s.getProps(objID)
			if err != nil {
				continue
			}
			out = append(out, childInfo{id: objID, name: name, size: size, isFolder: isFolder})
		}
		if fetched < batch {
			break
		}
	}
	return out, nil
}

// getProps reads the name, size, and folder-ness of one object. Must run on the
// executor thread.
func (s *wpdSession) getProps(objID string) (name string, size uint64, isFolder bool, err error) {
	// IPortableDeviceProperties::GetValues(pszObjectID, pKeys=NULL, ppValues) — index 5.
	// Passing NULL keys returns all available properties.
	var values uintptr
	if r := call(s.propsPtr, 5, uintptr(unsafe.Pointer(utf16Ptr(objID))), 0, uintptr(unsafe.Pointer(&values))); failed(r) {
		return "", 0, false, fmt.Errorf("read properties of %q: %w", objID, ole.NewError(r))
	}
	defer release(values)

	name = getStringValue(values, &keyOriginalFileName)
	if name == "" {
		name = getStringValue(values, &keyObjectName)
	}
	size = getU64Value(values, &keyObjectSize)

	var ct ole.GUID
	// IPortableDeviceValues::GetGuidValue(key, pValue) — index 28.
	if r := call(values, 28, uintptr(unsafe.Pointer(&keyContentType)), uintptr(unsafe.Pointer(&ct))); !failed(r) {
		isFolder = guidEqual(&ct, contentTypeFolder) || guidEqual(&ct, contentTypeFunctionalObject)
	}
	return name, size, isFolder, nil
}

func getStringValue(values uintptr, key *propertyKey) string {
	var p *uint16
	// IPortableDeviceValues::GetStringValue(key, pValue) — index 8.
	if r := call(values, 8, uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&p))); failed(r) || p == nil {
		return ""
	}
	defer ole.CoTaskMemFree(uintptr(unsafe.Pointer(p)))
	return utf16PtrToString(p)
}

func getU64Value(values uintptr, key *propertyKey) uint64 {
	var v uint64
	// IPortableDeviceValues::GetUnsignedLargeIntegerValue(key, pValue) — index 14.
	if r := call(values, 14, uintptr(unsafe.Pointer(key)), uintptr(unsafe.Pointer(&v))); failed(r) {
		return 0
	}
	return v
}

// list walks the session's folder collecting file objects. Must run on the
// executor thread.
func (s *wpdSession) list(recurse bool) ([]Object, error) {
	var objs []Object
	var walk func(parentID, prefix string) error
	walk = func(parentID, prefix string) error {
		children, err := s.children(parentID)
		if err != nil {
			return err
		}
		for _, c := range children {
			if c.isFolder {
				if recurse {
					if err := walk(c.id, prefix+c.name+"/"); err != nil {
						return err
					}
				}
				continue
			}
			objs = append(objs, &wpdObject{
				sess: s,
				id:   c.id,
				name: c.name,
				rel:  prefix + c.name,
				size: int64(c.size),
			})
		}
		return nil
	}
	if err := walk(s.rootObjectID, ""); err != nil {
		return nil, err
	}
	return objs, nil
}

func (s *wpdSession) List(recurse bool) ([]Object, error) {
	var objs []Object
	var err error
	s.exec.do(func() { objs, err = s.list(recurse) })
	return objs, err
}

func (s *wpdSession) releaseAll() {
	if s.devicePtr != 0 {
		// IPortableDevice::Close() — index 8.
		call(s.devicePtr, 8)
	}
	release(s.resourcesPtr)
	release(s.propsPtr)
	release(s.contentPtr)
	release(s.devicePtr)
	s.resourcesPtr, s.propsPtr, s.contentPtr, s.devicePtr = 0, 0, 0, 0
}

func (s *wpdSession) Close() error {
	s.exec.do(func() { s.releaseAll() })
	s.exec.stop()
	return nil
}

// wpdObject implements Object for a file on the device.
type wpdObject struct {
	sess *wpdSession
	id   string
	name string
	rel  string
	size int64
}

func (o *wpdObject) Name() string       { return o.name }
func (o *wpdObject) RelPath() string    { return o.rel }
func (o *wpdObject) Size() int64        { return o.size }
func (o *wpdObject) ModTime() time.Time { return time.Time{} }

func (o *wpdObject) Open() (io.ReadCloser, error) {
	var streamPtr uintptr
	var err error
	o.sess.exec.do(func() {
		var optimal uint32
		// IPortableDeviceResources::GetStream(pszObjectID, pKey, dwMode, pOptimalBufferSize, ppStream) — index 5.
		r := call(o.sess.resourcesPtr, 5,
			uintptr(unsafe.Pointer(utf16Ptr(o.id))),
			uintptr(unsafe.Pointer(&keyResourceDefault)),
			0, // STGM_READ
			uintptr(unsafe.Pointer(&optimal)),
			uintptr(unsafe.Pointer(&streamPtr)))
		if failed(r) {
			err = fmt.Errorf("open stream for %q: %w", o.name, ole.NewError(r))
		}
	})
	if err != nil {
		return nil, err
	}
	return &wpdStream{sess: o.sess, streamPtr: streamPtr}, nil
}

func (o *wpdObject) Delete() error {
	var err error
	o.sess.exec.do(func() {
		collUnk, e := ole.CreateInstance(clsidPropVariantCollection, iidPropVariantCollection)
		if e != nil {
			err = fmt.Errorf("create delete collection: %w", e)
			return
		}
		coll := uintptr(unsafe.Pointer(collUnk))
		defer release(coll)

		pv := propVariant{vt: vtLPWSTR, val: uintptr(unsafe.Pointer(utf16Ptr(o.id)))}
		// IPortableDevicePropVariantCollection::Add(pValue) — index 5.
		if r := call(coll, 5, uintptr(unsafe.Pointer(&pv))); failed(r) {
			err = fmt.Errorf("queue delete of %q: %w", o.name, ole.NewError(r))
			return
		}
		var results uintptr
		// IPortableDeviceContent::Delete(dwOptions=0 (no recursion), pObjectIDs, ppResults) — index 8.
		if r := call(o.sess.contentPtr, 8, 0, coll, uintptr(unsafe.Pointer(&results))); failed(r) {
			err = fmt.Errorf("delete %q from device: %w", o.name, ole.NewError(r))
			return
		}
		release(results)
	})
	return err
}

// wpdStream adapts an IStream to io.ReadCloser, marshaling reads onto the
// executor thread.
type wpdStream struct {
	sess      *wpdSession
	streamPtr uintptr
}

func (r *wpdStream) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	var n int
	var err error
	r.sess.exec.do(func() {
		var read uint32
		// ISequentialStream::Read(pv, cb, pcbRead) — index 3.
		hr := call(r.streamPtr, 3,
			uintptr(unsafe.Pointer(&p[0])),
			uintptr(len(p)),
			uintptr(unsafe.Pointer(&read)))
		if failed(hr) {
			err = ole.NewError(hr)
			return
		}
		n = int(read)
	})
	if err != nil {
		return 0, err
	}
	if n == 0 {
		return 0, io.EOF
	}
	return n, nil
}

func (r *wpdStream) Close() error {
	r.sess.exec.do(func() {
		release(r.streamPtr)
		r.streamPtr = 0
	})
	return nil
}
