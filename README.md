## FileFerry — media organizer (short)

FileFerry is a small CLI that organizes image and video files into target folders using metadata and filename patterns.

### What it does
- Scan one or more source directories (profiles) for media files.
- Extract metadata from filenames, EXIF (images) or ffprobe (videos) when available.
- Render a per-profile target path template and move (or copy) files (dry-run by default).
- Tidy up the organized tree afterwards, keeping it in line with the current config (`tidy:dupes`).

### Quick examples
Build and dry-run with your config file:

```bash
go build -o fileferry .
./fileferry config.yaml      # dry-run: shows what would move/copy
./fileferry config.yaml --ack # actually move/copy files
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
- `SourceConfig` has `path`, `recurse`, `types` and optional `filenames` and `operation`. `path` may be a local directory or an `mtp://` device URL (see "Android phone (MTP) sources").
- `operation` is either `move` (the default) or `copy`. A move deletes the source once the destination copy has been verified byte-for-byte; a copy leaves the source in place. Use `copy` when the source is a library you want to keep rather than an inbox to drain — e.g. mirroring a shared drive into your own organized tree:

```yaml
profiles:
  SharedDrive:
    sources:
      - path: /mnt/shared/photos
        recurse: true
        types: [image]
        operation: copy
    target:
      path: /organized/pictures/{meta.taken.year}/{meta.taken.date}/{meta.taken.datetime}.{file.extension}
```

  Either way the destination is verified by SHA-256 before anything is finalized, and a destination that already holds an identical file is reported as a duplicate (with a move, the redundant source is deleted; with a copy, nothing happens) — so re-running a `copy` profile is safe and idempotent. A destination that exists with *different* content is always an error that leaves both files untouched.
- `on_conflict` is either `error` (the default) or `keep-largest`; see "Conflicting renditions" below.
- `types` names are hierarchical: a type also covers its subtypes, so `image` matches standard images *and* RAW files (`image.raw`), while `image.raw` matches RAW files only. Consequently the same path cannot use `image` in one profile and `image.raw` in another — they would both claim the same files.

### Conflicting renditions (`on_conflict`)

Two *different* files can want the same target name — most often a camera original and an edit of the same shot (a Picasa or Lightroom crop keeps the original's `DateTimeOriginal`, `Make` and `Model`, so the template resolves both to the same path). By default that is an error and both files are left untouched, because picking a winner is usually a human decision.

Set `on_conflict: keep-largest` on a profile when the answer is always "the original wins":

```yaml
profiles:
  Pictures:
    on_conflict: keep-largest   # default: error
    sources:
      - path: /path/to/pictures
        types: [image]
    target:
      path: /organized/{meta.taken.year}/{meta.taken.date}/{meta.taken.datetime}.{file.extension}
```

The rendition with more pixels keeps the target path; the smaller one (the crop, the downscaled export) is parked beside it as `<name>-alt.<ext>`, then `-alt2`, `-alt3`. It works in both directions — an incoming crop is filed under the alt name and the original at the target path is never touched — so a smaller rendition can never displace a bigger one regardless of import order.

Two deliberate limits: if either side's dimensions cannot be read (RAW, HEIC — Go decodes JPEG/PNG/GIF headers only) or the two have the same pixel count, the policy declines to guess and falls back to the error, leaving both files alone. And re-importing a crop that is already parked is recognised as a duplicate rather than filed again, so repeated runs don't accumulate `-alt2`, `-alt3`.

`run --on-conflict=error|keep-largest` overrides every profile for a single run.

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

### Maintenance: `tidy`

`run` fills the target tree; the `tidy:*` commands keep it in line with the config as it exists *today*. Every one of them is dry-run by default and only acts with `--ack`.

#### `tidy:dupes` (alias `dupes`)

Finds byte-identical files sitting side by side in a profile's target tree and removes the redundant copies:

```bash
./fileferry dupes            # dry-run: shows what would be deleted, in every profile
./fileferry dupes Videos     # only the Videos profile
./fileferry dupes --ack      # actually delete
```

How it decides:

- **Where it looks.** The fixed directory prefix of the profile's `target.path` — everything before the first `{token}` — is the tree the profile owns; it is walked recursively for the file types the profile's sources claim.
- **What counts as a duplicate.** Files are grouped by directory and size first, and only same-size *siblings* are hashed, so a large library costs a walk rather than a full checksum pass. Siblings are enough: the target path is derived from the file's own content, so copies that belong together always land in the same folder. Equal size only makes two files candidates — SHA-256 decides.
- **Which copy survives.** The one already sitting where the current config says that content belongs, so the result is exactly what a fresh `run` of this config would have produced. The canonical path is resolved from the file's *content* (EXIF/ffprobe), which is authoritative because the copies are identical; filename patterns are consulted only when the content says nothing, and then only if the names that parse agree — two well-formed but different names cannot both be right, so neither is trusted.
- **When nothing is canonical.** If no copy is at its target path (usually because the template changed since they were written), one is kept by name order, the redundant ones are still removed, and the set is reported as still needing a rename.

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
