package updex

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

// TestPackagedCompletionsAndManPageAreWorldReadable pins package modes
// independently of the build umask (updex#553). The unit entries already pin 0644.
func TestPackagedCompletionsAndManPageAreWorldReadable(t *testing.T) {
	data, err := os.ReadFile("../.goreleaser.yaml")
	if err != nil {
		t.Fatalf("read .goreleaser.yaml: %v", err)
	}
	var config goreleaserConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		t.Fatalf("parse .goreleaser.yaml: %v", err)
	}
	if err := checkPackagedHelpModes(config); err != nil {
		t.Fatal(err)
	}
}

func checkPackagedHelpModes(config goreleaserConfig) error {
	if len(config.Nfpms) != 1 {
		return fmt.Errorf("nfpms has %d entries, want 1", len(config.Nfpms))
	}
	remaining := map[string]string{
		"./completions/updex.bash": "/etc/bash_completion.d/updex",
		"./completions/updex.fish": "/usr/share/fish/completions/updex.fish",
		"./completions/updex.zsh":  "/usr/share/zsh/site-functions/_updex",
		"./manpages/updex.1.gz":    "/usr/share/man/man1/updex.1.gz",
	}
	for _, content := range config.Nfpms[0].Contents {
		if !strings.HasPrefix(content.Src, "./completions/") && !strings.HasPrefix(content.Src, "./manpages/") {
			continue
		}
		wantDst, ok := remaining[content.Src]
		if !ok {
			return fmt.Errorf("nfpms.contents has an unexpected or duplicate src %q", content.Src)
		}
		if content.Dst != wantDst {
			return fmt.Errorf("nfpms content for %s has dst %q, want %q", content.Src, content.Dst, wantDst)
		}
		if content.FileInfo.Mode != "0644" {
			return fmt.Errorf("nfpms content for %s has mode %q, want 0644", content.Dst, content.FileInfo.Mode)
		}
		delete(remaining, content.Src)
	}
	if len(remaining) != 0 {
		return fmt.Errorf("nfpms.contents is missing src/dst pairs: %v", remaining)
	}
	return nil
}

func TestCheckPackagedHelpModes(t *testing.T) {
	const valid = `
nfpms:
  - contents:
      - {src: ./completions/updex.bash, dst: /etc/bash_completion.d/updex, file_info: {mode: 0644}}
      - {src: ./completions/updex.fish, dst: /usr/share/fish/completions/updex.fish, file_info: {mode: 0644}}
      - {src: ./completions/updex.zsh, dst: /usr/share/zsh/site-functions/_updex, file_info: {mode: 0644}}
      - {src: ./manpages/updex.1.gz, dst: /usr/share/man/man1/updex.1.gz, file_info: {mode: 0644}}
`
	tests := []struct {
		name    string
		change  func(*goreleaserConfig)
		wantErr string
	}{
		{name: "all four entries"},
		{name: "no package", change: func(cfg *goreleaserConfig) {
			cfg.Nfpms = nil
		}, wantErr: "nfpms has 0 entries, want 1"},
		{name: "missing entry", change: func(cfg *goreleaserConfig) {
			cfg.Nfpms[0].Contents = cfg.Nfpms[0].Contents[:3]
		}, wantErr: "missing src/dst pairs"},
		{name: "missing mode", change: func(cfg *goreleaserConfig) {
			cfg.Nfpms[0].Contents[0].FileInfo.Mode = ""
		}, wantErr: `/etc/bash_completion.d/updex has mode "", want 0644`},
		{name: "private mode", change: func(cfg *goreleaserConfig) {
			cfg.Nfpms[0].Contents[0].FileInfo.Mode = "0600"
		}, wantErr: `/etc/bash_completion.d/updex has mode "0600", want 0644`},
		{name: "wrong destination", change: func(cfg *goreleaserConfig) {
			cfg.Nfpms[0].Contents[0].Dst = "/wrong"
		}, wantErr: `has dst "/wrong", want "/etc/bash_completion.d/updex"`},
		{name: "extra entry", change: func(cfg *goreleaserConfig) {
			extra := cfg.Nfpms[0].Contents[0]
			extra.Src = "./completions/updex.nu"
			extra.Dst = "/usr/share/nushell/completions/updex.nu"
			cfg.Nfpms[0].Contents = append(cfg.Nfpms[0].Contents, extra)
		}, wantErr: `unexpected or duplicate src "./completions/updex.nu"`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var config goreleaserConfig
			if err := yaml.Unmarshal([]byte(valid), &config); err != nil {
				t.Fatal(err)
			}
			if test.change != nil {
				test.change(&config)
			}
			err := checkPackagedHelpModes(config)
			if test.wantErr == "" {
				if err != nil {
					t.Fatal(err)
				}
				return
			}
			if err == nil || !strings.Contains(err.Error(), test.wantErr) {
				t.Errorf("checkPackagedHelpModes: got %v, want an error containing %q", err, test.wantErr)
			}
		})
	}
}
