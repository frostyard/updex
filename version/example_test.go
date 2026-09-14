package version_test

import (
	"fmt"

	"github.com/frostyard/updex/v2/version"
)

func ExampleParsePattern() {
	pattern, err := version.ParsePattern("toolbox-@v-x86-64.raw.xz")
	if err != nil {
		fmt.Println("parse pattern:", err)
		return
	}
	fmt.Println(pattern.Raw())

	_, err = version.ParsePattern("toolbox-1.4.0-x86-64.raw.xz")
	fmt.Println(err)

	// Output:
	// toolbox-@v-x86-64.raw.xz
	// pattern must contain @v placeholder
}

func ExamplePattern_ExtractVersion() {
	pattern, err := version.ParsePattern("toolbox-@v-x86-64.raw.xz")
	if err != nil {
		fmt.Println("parse pattern:", err)
		return
	}

	for _, filename := range []string{
		"toolbox-2.3.1-x86-64.raw.xz",
		"toolbox-latest-arm64.raw.xz",
	} {
		availableVersion, matched := pattern.ExtractVersion(filename)
		fmt.Printf("%s: version=%q matched=%t\n", filename, availableVersion, matched)
	}

	// Output:
	// toolbox-2.3.1-x86-64.raw.xz: version="2.3.1" matched=true
	// toolbox-latest-arm64.raw.xz: version="" matched=false
}

func ExamplePattern_BuildFilename() {
	pattern, err := version.ParsePattern("toolbox-@v-x86-64.raw.xz")
	if err != nil {
		fmt.Println("parse pattern:", err)
		return
	}

	fmt.Println(pattern.BuildFilename("2.4.0"))

	// Output:
	// toolbox-2.4.0-x86-64.raw.xz
}

func ExampleSort() {
	availableVersions := []string{"2.3.1", "1.9.0", "2.4.0-rc1", "2.4.0"}
	version.Sort(availableVersions)
	fmt.Println(availableVersions)

	// Output:
	// [2.4.0 2.4.0-rc1 2.3.1 1.9.0]
}
