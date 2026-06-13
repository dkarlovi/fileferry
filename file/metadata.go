package file

import (
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	mp4 "github.com/abema/go-mp4"
	mkvparse "github.com/remko/go-mkvparse"
	"github.com/rwcarlsen/goexif/exif"
)

type FileMetadata struct {
	TakenTime   *time.Time
	Extension   string
	CameraMaker string
	CameraModel string
}

type FilenameMetaRule struct {
	Path   string
	Exp    string
	Format string // Optional: time format for parsing (e.g., "15-04-05" for HH-MM-SS)
}

// FilenameMetaFormat represents a format variant for a metadata token
type FilenameMetaFormat struct {
	Specifier  string // Format specifier (e.g., "hhmmss", "hh-mm-ss")
	Regex      string // Regex pattern to match
	TimeLayout string // Go time layout for parsing
}

// FilenameMetaFormatVariants defines format variants for metadata tokens that support custom parsing
var FilenameMetaFormatVariants = map[string][]FilenameMetaFormat{
	"meta.taken.date": {
		{Specifier: "yyyy-mm-dd", Regex: `\d{4}-\d{2}-\d{2}`, TimeLayout: "2006-01-02"},
		{Specifier: "yyyymmdd", Regex: `\d{8}`, TimeLayout: "20060102"},
	},
	"meta.taken.time": {
		{Specifier: "hh-mm-ss", Regex: `\d{2}-\d{2}-\d{2}`, TimeLayout: "15-04-05"},
		{Specifier: "hhmmss", Regex: `\d{6}`, TimeLayout: "150405"},
		{Specifier: "hhmm", Regex: `\d{4}`, TimeLayout: "1504"},
	},
}

var FilenameMetaRules = []FilenameMetaRule{
	{Path: "meta.taken.date", Exp: `\d{4}-\d{2}-\d{2}`, Format: "2006-01-02"},
	{Path: "meta.taken.time", Exp: `\d{2}-\d{2}-\d{2}`, Format: "15-04-05"},
}

// handler to capture DateUTC element from Matroska files
type dateHandler struct {
	mkvparse.DefaultHandler
	found bool
	tm    time.Time
}

func (h *dateHandler) HandleDate(id mkvparse.ElementID, v time.Time, info mkvparse.ElementInfo) error {
	if id == mkvparse.DateUTCElement && !h.found {
		h.found = true
		h.tm = v
		// return nil and let parser finish or short-circuit by returning a special error? mkvparse doesn't define short-circuit error, so we capture and let it finish
	}
	return nil
}

// normalizeExt lowercases an extension and strips a leading dot.
func normalizeExt(ext string) string {
	return strings.TrimPrefix(strings.ToLower(ext), ".")
}

// extractImageMetadata reads image metadata from a file on disk. It is a thin
// wrapper around extractImageMetadataFromEntry kept for direct path callers
// (and tests).
func extractImageMetadata(path string) (*FileMetadata, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return &FileMetadata{Extension: normalizeExt(filepath.Ext(path))}, nil
	}
	return extractImageMetadataFromEntry(&localEntry{path: path, info: fi})
}

// extractImageMetadataFromEntry reads image EXIF metadata by streaming the
// entry's content. This works for JPEG and TIFF-based RAW (DNG, ARW, …) over
// any source, including MTP. The exiftool fallback needs a real path, so it
// runs only for entries that expose one (local files).
func extractImageMetadataFromEntry(e Entry) (*FileMetadata, error) {
	meta := &FileMetadata{Extension: normalizeExt(filepath.Ext(e.Name()))}

	rc, err := e.Open()
	if err == nil {
		func() {
			defer rc.Close()
			x, err := exif.Decode(rc)
			if err != nil {
				return
			}
			if tm, err := x.DateTime(); err == nil {
				localTm := tm.Local()
				meta.TakenTime = &localTm
			}
			if maker, err := x.Get(exif.Make); err == nil {
				if makerStr, err := maker.StringVal(); err == nil {
					meta.CameraMaker = strings.TrimSpace(makerStr)
				}
			}
			if model, err := x.Get(exif.Model); err == nil {
				if modelStr, err := model.StringVal(); err == nil {
					meta.CameraModel = strings.TrimSpace(modelStr)
				}
			}
		}()
	}

	// Fallback to exiftool if direct EXIF reading failed or didn't get all data.
	// Only available when the entry is backed by a real filesystem path.
	if meta.TakenTime == nil || meta.CameraMaker == "" || meta.CameraModel == "" {
		if lp, ok := e.(localPathProvider); ok {
			if exiftoolMeta := extractImageMetadataWithExiftool(lp.LocalPath()); exiftoolMeta != nil {
				if meta.TakenTime == nil && exiftoolMeta.TakenTime != nil {
					meta.TakenTime = exiftoolMeta.TakenTime
				}
				if meta.CameraMaker == "" && exiftoolMeta.CameraMaker != "" {
					meta.CameraMaker = exiftoolMeta.CameraMaker
				}
				if meta.CameraModel == "" && exiftoolMeta.CameraModel != "" {
					meta.CameraModel = exiftoolMeta.CameraModel
				}
			}
		}
	}

	return meta, nil
}

// extractImageMetadataWithExiftool uses exiftool command as fallback for EXIF extraction
func extractImageMetadataWithExiftool(path string) *FileMetadata {
	cmd := exec.Command("exiftool", "-j", "-CreateDate", "-Make", "-Model", path)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}

	var result []map[string]interface{}
	if err := json.Unmarshal(out, &result); err != nil || len(result) == 0 {
		return nil
	}

	data := result[0]
	meta := &FileMetadata{}

	// Extract creation date
	if createDate, ok := data["CreateDate"].(string); ok && createDate != "" {
		// Try different date formats that exiftool might return
		layouts := []string{
			"2006:01:02 15:04:05",
			"2006-01-02 15:04:05",
			time.RFC3339,
		}
		for _, layout := range layouts {
			if tm, err := time.ParseInLocation(layout, createDate, time.Local); err == nil {
				meta.TakenTime = &tm
				break
			}
		}
	}

	// Extract camera maker
	if make, ok := data["Make"].(string); ok {
		meta.CameraMaker = strings.TrimSpace(make)
	}

	// Extract camera model
	if model, ok := data["Model"].(string); ok {
		meta.CameraModel = strings.TrimSpace(model)
	}

	return meta
}

// extractVideoMetadata reads video metadata from a file on disk. Thin wrapper
// around extractVideoMetadataFromEntry kept for direct path callers (and tests).
func extractVideoMetadata(path string) (*FileMetadata, error) {
	fi, err := os.Stat(path)
	if err != nil {
		return &FileMetadata{Extension: normalizeExt(filepath.Ext(path))}, nil
	}
	return extractVideoMetadataFromEntry(&localEntry{path: path, info: fi})
}

// extractVideoMetadataFromEntry reads video container metadata for the entry.
// The mp4/mkv box parsers need random access, so for streamed (MTP) sources the
// content is spooled to a temp file via asReaderAt. The ffprobe fallback needs a
// real path and runs only for entries that expose one (local files).
func extractVideoMetadataFromEntry(e Entry) (*FileMetadata, error) {
	ext := normalizeExt(filepath.Ext(e.Name()))
	meta := &FileMetadata{Extension: ext}

	if ra, size, cleanup, err := asReaderAt(e); err == nil {
		defer cleanup()
		parseVideoBoxes(ra, size, ext, meta)
	}

	if meta.TakenTime == nil {
		if lp, ok := e.(localPathProvider); ok {
			parseVideoWithFfprobe(lp.LocalPath(), meta)
		}
	}

	return meta, nil
}

// asReaderAt returns random-access content for an entry plus its size. Local
// files are opened directly; streamed sources (MTP) are spooled to a temp file.
// cleanup must always be called when err is nil.
func asReaderAt(e Entry) (ra io.ReaderAt, size int64, cleanup func(), err error) {
	if lp, ok := e.(localPathProvider); ok {
		f, err := os.Open(lp.LocalPath())
		if err != nil {
			return nil, 0, nil, err
		}
		fi, err := f.Stat()
		if err != nil {
			f.Close()
			return nil, 0, nil, err
		}
		return f, fi.Size(), func() { f.Close() }, nil
	}

	rc, err := e.Open()
	if err != nil {
		return nil, 0, nil, err
	}
	defer rc.Close()
	tmp, err := os.CreateTemp("", "fileferry-video-*")
	if err != nil {
		return nil, 0, nil, err
	}
	n, err := io.Copy(tmp, rc)
	if err != nil {
		tmp.Close()
		os.Remove(tmp.Name())
		return nil, 0, nil, err
	}
	return tmp, n, func() { tmp.Close(); os.Remove(tmp.Name()) }, nil
}

// parseVideoBoxes fills meta.TakenTime from mp4/mkv container metadata.
func parseVideoBoxes(ra io.ReaderAt, size int64, ext string, meta *FileMetadata) {
	rs := io.NewSectionReader(ra, 0, size)
	switch ext {
	case "mp4", "m4v", "mov":
		boxes, err := mp4.ExtractBoxWithPayload(rs, nil, mp4.BoxPath{mp4.BoxTypeMoov(), mp4.BoxTypeMvhd()})
		if err == nil && len(boxes) > 0 {
			if mvhd, ok := boxes[0].Payload.(*mp4.Mvhd); ok {
				var creationSecs uint64
				if mvhd.CreationTimeV1 != 0 {
					creationSecs = uint64(mvhd.CreationTimeV1)
				} else if mvhd.CreationTimeV0 != 0 {
					creationSecs = uint64(mvhd.CreationTimeV0)
				}
				if creationSecs != 0 {
					epoch1904 := time.Date(1904, 1, 1, 0, 0, 0, 0, time.UTC)
					tm := epoch1904.Add(time.Duration(creationSecs) * time.Second).Local()
					meta.TakenTime = &tm
				}
			}
		}
	case "mkv", "webm":
		dh := &dateHandler{}
		if err := mkvparse.Parse(rs, dh); err == nil && dh.found {
			localTm := dh.tm.Local()
			meta.TakenTime = &localTm
		}
	}
}

// parseVideoWithFfprobe fills meta from ffprobe output (camera maker/model and
// creation time). Requires a real filesystem path.
func parseVideoWithFfprobe(path string, meta *FileMetadata) {
	cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", path)
	out, err := cmd.Output()
	if err != nil {
		return
	}
	var ffprobe struct {
		Format struct {
			Tags map[string]string `json:"tags"`
		} `json:"format"`
	}
	if err := json.Unmarshal(out, &ffprobe); err != nil {
		return
	}
	tags := ffprobe.Format.Tags

	lookup := func(keys ...string) string {
		for _, k := range keys {
			if v, ok := tags[k]; ok && v != "" {
				return v
			}
		}
		return ""
	}

	meta.CameraMaker = lookup("com.android.manufacturer", "make", "manufacturer")
	meta.CameraModel = lookup("com.android.model", "model")

	ct := lookup("creation_time")
	if ct != "" {
		layouts := []string{
			time.RFC3339,
			"2006-01-02T15:04:05.000000Z",
			"2006-01-02 15:04:05",
		}
		for _, layout := range layouts {
			if tm, err := time.Parse(layout, ct); err == nil {
				localTm := tm.Local()
				meta.TakenTime = &localTm
				break
			}
		}
	}
}

func parseMetadataFromFilenamePattern(filename, pattern string) *FileMetadata {
	ext := filepath.Ext(filename)
	input := filename
	regexPattern := pattern
	groupMap := make(map[string]string)
	formatMap := make(map[string]string) // Maps token path to its time format

	// First, handle tokens with format specifiers (e.g., {meta.taken.time:hhmmss})
	tokenWithFormatRegex := regexp.MustCompile(`\{([^}:]+):([^}]+)\}`)
	matches := tokenWithFormatRegex.FindAllStringSubmatch(pattern, -1)
	for _, match := range matches {
		fullToken := match[0]  // e.g., "{meta.taken.time:hhmmss}"
		tokenPath := match[1]  // e.g., "meta.taken.time"
		formatSpec := match[2] // e.g., "hhmmss"

		// Check if this token supports format variants
		if variants, ok := FilenameMetaFormatVariants[tokenPath]; ok {
			// Find the matching format variant
			for _, variant := range variants {
				if variant.Specifier == formatSpec {
					grp := strings.ReplaceAll(tokenPath, ".", "_")
					regexPattern = strings.Replace(regexPattern, fullToken, "(?P<"+grp+">"+variant.Regex+")", 1)
					groupMap[grp] = tokenPath
					formatMap[tokenPath] = variant.TimeLayout
					break
				}
			}
		}
	}

	// Then, handle tokens without format specifiers (use default patterns)
	for _, rule := range FilenameMetaRules {
		tokenWithoutFormat := "{" + rule.Path + "}"
		if strings.Contains(regexPattern, tokenWithoutFormat) {
			grp := strings.ReplaceAll(rule.Path, ".", "_")
			regexPattern = strings.ReplaceAll(regexPattern, tokenWithoutFormat, "(?P<"+grp+">"+rule.Exp+")")
			groupMap[grp] = rule.Path
			if rule.Format != "" {
				formatMap[rule.Path] = rule.Format
			}
		}
	}

	regexPattern = "^" + regexPattern + "$"
	re, err := regexp.Compile(regexPattern)
	if err != nil {
		return nil
	}
	match := re.FindStringSubmatch(input)
	if match == nil {
		return nil
	}
	groups := make(map[string]string)
	for i, n := range re.SubexpNames() {
		if i > 0 && n != "" {
			if orig, ok := groupMap[n]; ok {
				groups[orig] = match[i]
			} else {
				groups[n] = match[i]
			}
		}
	}
	meta := &FileMetadata{
		Extension: strings.TrimPrefix(ext, "."),
	}
	if date, ok := groups["meta.taken.date"]; ok {
		dateFormat := formatMap["meta.taken.date"]
		if dateFormat == "" {
			dateFormat = "2006-01-02" // default
		}

		if t, ok2 := groups["meta.taken.time"]; ok2 {
			timeFormat := formatMap["meta.taken.time"]
			if timeFormat == "" {
				timeFormat = "15-04-05" // default
			}
			tm, err := time.ParseInLocation(dateFormat+" "+timeFormat, date+" "+t, time.Local)
			if err == nil {
				meta.TakenTime = &tm
			}
		} else {
			tm, err := time.ParseInLocation(dateFormat, date, time.Local)
			if err == nil {
				meta.TakenTime = &tm
			}
		}
	}
	if meta.TakenTime != nil {
		return meta
	}
	return nil
}
