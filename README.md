## FileFerry — media organizer (short)

FileFerry is a small CLI that organizes image and video files into target folders using metadata and filename patterns.

### What it does
- Scan one or more source directories (profiles) for media files.
- Extract metadata from filenames, EXIF (images) or ffprobe (videos) when available.
- Render a per-profile target path template and move files (dry-run by default).

### Quick examples
Build and dry-run with your config file:

```bash
go build -o fileferry .
./fileferry config.yaml      # dry-run: shows what would move
./fileferry config.yaml --ack # actually move files
```

Example (anonymized) `config.yaml` excerpt:

```yaml
profiles:
  Videos:
    sources:
      - path: /path/to/videos
        recurse: true
        types: [video]
    patterns:
      - "{meta.taken.date} {meta.taken.time}.mkv"
      - "{meta.taken.date}-{meta.taken.time}.mkv"
    target:
      path: /organized/videos/{meta.taken.year}/{meta.taken.date}/{meta.taken.datetime}.{file.extension}

  Pictures:
    sources:
      - path: /path/to/pictures
        recurse: false
        types: [image]
    target:
      path: /organized/pictures/{meta.taken.year}/{meta.taken.date}/{meta.taken.datetime}.{file.extension}
```

### Config contract (short)
- `profiles` is a map of profile names -> profile config.
- A `ProfileConfig` contains: `sources` (list), optional `patterns` (filename patterns used to extract metadata), and `target.path` (template used to build destination path).
- `SourceConfig` has `path`, `recurse`, `types` and optional `filenames`. `path` may be a local directory or an `mtp://` device URL (see "Android phone (MTP) sources").

### Android phone (MTP) sources — Windows only

You can scan a connected Android phone (or any MTP device) directly as a source,
using an `mtp://` URL in place of a filesystem path:

```yaml
profiles:
  PhoneRAWs:
    sources:
      - path: "mtp://Pixel 9 Pro/Internal shared storage/DCIM/Camera"
        recurse: true
        types: [image.raw]
    target:
      path: /organized/raw/{meta.taken.year}/{meta.taken.date}/{meta.taken.datetime}.{file.extension}
```

The URL is `mtp://<device friendly name>/<on-device folder>`, where the device
name and folder path are exactly what Windows Explorer shows (e.g. *This PC ▸
Pixel 9 Pro ▸ Internal shared storage ▸ DCIM ▸ Camera*). This is handy for
pulling just the RAW files off the phone — something many photo apps can't do,
because an MTP device is not a real drive (no `C:`/`G:`); it's exposed only
through the Windows Portable Devices (WPD) COM API.

Notes and caveats:
- **Windows only, and native Windows only.** The phone is owned by the Windows
  host's WPD driver, so this requires running `fileferry.exe` directly on
  Windows. It does **not** work from WSL (the device isn't visible inside the
  WSL VM). On non-Windows builds an `mtp://` source fails with a clear error.
- **Move semantics for MTP.** Files are copied off the device, verified by
  re-reading the source and comparing SHA-256 against the local copy, and only
  then deleted from the phone. If verification fails, nothing is deleted.
- Metadata for MTP files comes from EXIF (read directly over MTP, works for
  JPEG and TIFF-based RAW such as DNG/ARW) and from filename patterns. The
  `exiftool`/`ffprobe` fallbacks apply to local sources only.
- Unlock the phone and set its USB mode to *File Transfer* before running.

### Template variables
- `{meta.taken.year}`, `{meta.taken.date}`, `{meta.taken.datetime}`
- `{meta.camera.maker}`, `{meta.camera.model}`
- `{file.extension}` (no leading dot)

Notes: filename patterns are anchored and must match the filename exactly (e.g. `2025-06-02 15-21-02.mkv`). Patterns support tokens like `{meta.taken.date}` and `{meta.taken.time}` which map to regex rules.

### Custom format specifiers
Some tokens support custom format specifiers to match different time formats. Format specifiers are specified after a colon in the token (e.g., `{meta.taken.time:hhmmss}`).

Supported format specifiers for `{meta.taken.time}`:
- `hh-mm-ss` (default): Time in format `HH-MM-SS` (e.g., `22-22-12`)
- `hhmmss`: Time in compact format `HHMMSS` (e.g., `222212`)
- `hhmm`: Time in hour-minute format `HHMM` (e.g., `1530`)

Example using custom format specifiers:
```yaml
profiles:
  Pictures:
    sources:
      - path: /path/to/pictures
        recurse: false
        types: [image]
    patterns:
      - "Still {meta.taken.date} {meta.taken.time:hhmmss}_1.1.1.jpg"
    target:
      path: /organized/{meta.taken.year}/{meta.taken.date}/{meta.taken.datetime}.{file.extension}
```
This pattern matches filenames like `Still 2026-01-23 222212_1.1.1.jpg` where the time `222212` represents `22:22:12`.

### Build & lint

```bash
go build -o fileferry .
gofmt -d .
go vet ./...
```

### External tools (optional)
- `ffprobe` (from ffmpeg) improves video metadata extraction.
- `exiftool` improves image metadata extraction.

### Validation
- The config loader validates that each profile has a non-empty `target.path`, that source paths are unique across profiles, and that any `mtp://` source URL is well-formed.

Short and to the point — see the source and `config.yaml` for details.
