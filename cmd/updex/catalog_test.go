package updex

import "testing"

func TestSplitCatalogArg(t *testing.T) {
	tests := []struct {
		arg      string
		repoFlag string
		wantRepo string
		wantName string
		wantErr  bool
	}{
		{arg: "zoxide", wantRepo: "", wantName: "zoxide"},
		{arg: "zoxide", repoFlag: "fedora", wantRepo: "fedora", wantName: "zoxide"},
		{arg: "fedora/zoxide", wantRepo: "fedora", wantName: "zoxide"},
		{arg: "fedora/zoxide", repoFlag: "fedora", wantRepo: "fedora", wantName: "zoxide"},
		{arg: "fedora/zoxide", repoFlag: "community", wantErr: true},
		{arg: "/zoxide", wantErr: true},
		{arg: "fedora/", wantErr: true},
		{arg: "a/b/c", wantErr: true},
	}

	for _, tt := range tests {
		repo, name, err := splitCatalogArg(tt.arg, tt.repoFlag)
		if tt.wantErr {
			if err == nil {
				t.Errorf("splitCatalogArg(%q, %q): expected error", tt.arg, tt.repoFlag)
			}
			continue
		}
		if err != nil {
			t.Errorf("splitCatalogArg(%q, %q): unexpected error %v", tt.arg, tt.repoFlag, err)
			continue
		}
		if repo != tt.wantRepo || name != tt.wantName {
			t.Errorf("splitCatalogArg(%q, %q) = (%q, %q), want (%q, %q)",
				tt.arg, tt.repoFlag, repo, name, tt.wantRepo, tt.wantName)
		}
	}
}
