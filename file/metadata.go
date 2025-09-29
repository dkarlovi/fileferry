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
	Path string
	Exp  string
}

var FilenameMetaRules = []FilenameMetaRule{
	{Path: "meta.taken.date", Exp: `\d{4}-\d{2}-\d{2}`},
	{Path: "meta.taken.time", Exp: `\d{2}-\d{2}-\d{2}`},
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

func extractImageMetadata(path string) (*FileMetadata, error) {
	meta := &FileMetadata{
		Extension: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}

	// Try to extract EXIF data directly using Go library
	f, err := os.Open(path)
	if err != nil {
		return meta, nil
	}
	defer f.Close()

	x, err := exif.Decode(f)
	if err == nil {
		// Extract creation date/time
		if tm, err := x.DateTime(); err == nil {
			localTm := tm.Local()
			meta.TakenTime = &localTm
		}

		// Extract camera maker
		if maker, err := x.Get(exif.Make); err == nil {
			if makerStr, err := maker.StringVal(); err == nil {
				meta.CameraMaker = strings.TrimSpace(makerStr)
			}
		}

		// Extract camera model
		if model, err := x.Get(exif.Model); err == nil {
			if modelStr, err := model.StringVal(); err == nil {
				meta.CameraModel = strings.TrimSpace(modelStr)
			}
		}
	}

	// Fallback to exiftool if direct EXIF reading failed or didn't get all data
	if meta.TakenTime == nil || meta.CameraMaker == "" || meta.CameraModel == "" {
		if exiftoolMeta := extractImageMetadataWithExiftool(path); exiftoolMeta != nil {
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

func extractVideoMetadata(path string) (*FileMetadata, error) {
	meta := &FileMetadata{
		Extension: strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), "."),
	}
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(path), "."))
	f, err := os.Open(path)
	if err == nil {
		defer f.Close()
		rs := io.NewSectionReader(f, 0, 1<<62)
		switch ext {
		case "mp4", "m4v", "mov":
			boxes, err2 := mp4.ExtractBoxWithPayload(rs, nil, mp4.BoxPath{mp4.BoxTypeMoov(), mp4.BoxTypeMvhd()})
			if err2 == nil && len(boxes) > 0 {
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
			if err2 := mkvparse.Parse(rs, dh); err2 == nil && dh.found {
				localTm := dh.tm.Local()
				meta.TakenTime = &localTm
			}
		}
	}

	if meta.TakenTime == nil {
		cmd := exec.Command("ffprobe", "-v", "quiet", "-print_format", "json", "-show_format", path)
		out, err := cmd.Output()
		if err != nil {
			return meta, nil
		}
		var ffprobe struct {
			Format struct {
				Tags map[string]string `json:"tags"`
			} `json:"format"`
		}
		if err := json.Unmarshal(out, &ffprobe); err != nil {
			return meta, nil
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

		makerKeys := []string{"com.android.manufacturer", "make", "manufacturer"}
		modelKeys := []string{"com.android.model", "model"}
		creationTimeKeys := []string{"creation_time"}

		meta.CameraMaker = lookup(makerKeys...)
		meta.CameraModel = lookup(modelKeys...)

		ct := lookup(creationTimeKeys...)
		if ct != "" {
			layouts := []string{
				time.RFC3339,
				"2006-01-02T15:04:05.000000Z",
				"2006-01-02 15:04:05",
			}
			var tm time.Time
			var parseErr error
			for _, layout := range layouts {
				tm, parseErr = time.Parse(layout, ct)
				if parseErr == nil {
					localTm := tm.Local()
					meta.TakenTime = &localTm
					break
				}
			}
		}
	}

	return meta, nil
}

func parseMetadataFromFilenamePattern(filename, pattern string) *FileMetadata {
	ext := filepath.Ext(filename)
	input := filename
	regexPattern := pattern
	groupMap := make(map[string]string)
	for _, rule := range FilenameMetaRules {
		grp := strings.ReplaceAll(rule.Path, ".", "_")
		regexPattern = strings.ReplaceAll(regexPattern, "{"+rule.Path+"}", "(?P<"+grp+">"+rule.Exp+")")
		groupMap[grp] = rule.Path
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
		if t, ok2 := groups["meta.taken.time"]; ok2 {
			tm, err := time.ParseInLocation("2006-01-02 15-04-05", date+" "+t, time.Local)
			if err == nil {
				meta.TakenTime = &tm
			}
		} else {
			tm, err := time.ParseInLocation("2006-01-02", date, time.Local)
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
