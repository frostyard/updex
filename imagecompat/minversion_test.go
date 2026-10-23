package imagecompat

import (
	"strings"
	"testing"
)

func TestCheckMinImageVersion(t *testing.T) {
	const img = "20261022021319"
	long := strings.Repeat("9", 30)
	tests := []struct {
		name       string
		release    map[string]string
		image      string
		wantOK     bool
		wantReason string
	}{
		{name: "key absent", release: map[string]string{"ID": "x"}, image: img, wantOK: true},
		{name: "key absent, image version empty", release: nil, image: "", wantOK: true},
		{name: "new enough", release: minKey("20261001000000"), image: img, wantOK: true},
		{name: "equal", release: minKey(img), image: img, wantOK: true},
		{name: "too old", release: minKey("20261023000000"), image: img, wantReason: "needs image >= 20261023000000"},
		{name: "too old by one", release: minKey("20261022021320"), image: img, wantReason: "needs image >= 20261022021320"},
		{name: "min not numeric", release: minKey("v2"), image: img, wantReason: "needs image >= v2"},
		{name: "min has a sign", release: minKey("+5"), image: img, wantReason: "needs image >= +5"},
		{name: "min has a space", release: minKey("5 "), image: img, wantReason: "needs image >= 5 "},
		{name: "min is empty", release: minKey(""), image: img, wantReason: "needs image >= "},
		{name: "min is a decimal", release: minKey("1.5"), image: img, wantReason: "needs image >= 1.5"},
		{name: "image not numeric", release: minKey("20261001000000"), image: "20261022-1", wantReason: "needs image >= 20261001000000"},
		{name: "image negative", release: minKey("1"), image: "-5", wantReason: "needs image >= 1"},
		{name: "image version empty", release: minKey("20261001000000"), image: "", wantReason: "needs image >= 20261001000000"},
		{name: "image has a space", release: minKey("1"), image: " 2", wantReason: "needs image >= 1"},
		{name: "leading zeros, equal", release: minKey("0020261022021319"), image: img, wantOK: true},
		{name: "leading zeros on the image", release: minKey(img), image: "00" + img, wantOK: true},
		{name: "leading zeros, too old", release: minKey("0000000000000000000000000000002"), image: "1", wantReason: "needs image >= 0000000000000000000000000000002"},
		{name: "zero min", release: minKey("0"), image: "0", wantOK: true},
		{name: "all zeros", release: minKey("000"), image: "0", wantOK: true},
		{name: "30 digits, new enough", release: minKey(long), image: long, wantOK: true},
		{name: "30 digits min, short image", release: minKey(long), image: img, wantReason: "needs image >= " + long},
		{name: "30 digits image, short min", release: minKey(img), image: long, wantOK: true},
		{name: "21 digits differ in the last digit", release: minKey("100000000000000000002"), image: "100000000000000000001", wantReason: "needs image >= 100000000000000000002"},
		{name: "beyond uint64", release: minKey("18446744073709551616"), image: "18446744073709551615", wantReason: "needs image >= 18446744073709551616"},
		{name: "beyond uint64, equal plus one", release: minKey("18446744073709551615"), image: "18446744073709551616", wantOK: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reason := CheckMinImageVersion(tt.release, tt.image)
			if ok != tt.wantOK || reason != tt.wantReason {
				t.Errorf("got (%v, %q), want (%v, %q)", ok, reason, tt.wantOK, tt.wantReason)
			}
		})
	}
}

func TestCheckMinImageVersionSanitizesReason(t *testing.T) {
	_, reason := CheckMinImageVersion(minKey("1\x1b[31m\nred"), "5")
	if strings.ContainsAny(reason, "\x1b\n") {
		t.Errorf("reason has control characters: %q", reason)
	}
	_, reason = CheckMinImageVersion(minKey(strings.Repeat("7", 500)), "5")
	if len(reason) > len("needs image >= ")+maxDisplayLen {
		t.Errorf("reason is %d bytes", len(reason))
	}
}

func TestMinImageVersion(t *testing.T) {
	if v, set := MinImageVersion(map[string]string{MinImageVersionKey: ""}); !set || v != "" {
		t.Errorf("empty value: got (%q, %v)", v, set)
	}
	if _, set := MinImageVersion(nil); set {
		t.Error("nil map reports the key as set")
	}
	if MinImageVersionKey != "UPDEX_MIN_IMAGE_VERSION" {
		t.Errorf("key = %q", MinImageVersionKey)
	}
}

func minKey(v string) map[string]string { return map[string]string{MinImageVersionKey: v} }
