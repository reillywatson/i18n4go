package input_files

import (
	"fmt"
	"path/filepath"

	goi18n "github.com/EverlongProject/go-i18n/i18n"
	"github.com/EverlongProject/i18n4go/i18n"
)

var T goi18n.TranslateFunc

func init() {
	fmt.Println("DEBUG: this is a test i18n_init.go file")
	T = i18n.Init(filepath.Join("test_fixtures", "rewrite_package", "init_code_snippet_filename", "input_files"), i18n.GetResourcesPath())
}
