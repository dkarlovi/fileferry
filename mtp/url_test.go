package mtp

import "testing"

func TestParseURL(t *testing.T) {
	tests := []struct {
		name       string
		raw        string
		wantDevice string
		wantPath   string
		wantErr    bool
	}{
		{
			name:       "typical pixel path",
			raw:        "mtp://Pixel 9 Pro/Internal shared storage/DCIM/Camera",
			wantDevice: "Pixel 9 Pro",
			wantPath:   "Internal shared storage/DCIM/Camera",
		},
		{
			name:       "trailing slash trimmed",
			raw:        "mtp://Pixel 9 Pro/DCIM/Camera/",
			wantDevice: "Pixel 9 Pro",
			wantPath:   "DCIM/Camera",
		},
		{
			name:       "single-segment path",
			raw:        "mtp://MyPhone/DCIM",
			wantDevice: "MyPhone",
			wantPath:   "DCIM",
		},
		{name: "missing scheme", raw: "Pixel 9 Pro/DCIM", wantErr: true},
		{name: "device only", raw: "mtp://Pixel 9 Pro", wantErr: true},
		{name: "device only trailing slash", raw: "mtp://Pixel 9 Pro/", wantErr: true},
		{name: "empty", raw: "mtp://", wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			device, path, err := ParseURL(tt.raw)
			if (err != nil) != tt.wantErr {
				t.Fatalf("ParseURL(%q) error = %v, wantErr %v", tt.raw, err, tt.wantErr)
			}
			if tt.wantErr {
				return
			}
			if device != tt.wantDevice {
				t.Errorf("device = %q, want %q", device, tt.wantDevice)
			}
			if path != tt.wantPath {
				t.Errorf("path = %q, want %q", path, tt.wantPath)
			}
		})
	}
}

func TestIsURL(t *testing.T) {
	if !IsURL("mtp://Phone/DCIM") {
		t.Error("IsURL should be true for mtp:// path")
	}
	if IsURL("/home/user/photos") {
		t.Error("IsURL should be false for local path")
	}
}
