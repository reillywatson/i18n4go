package extract_strings

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"

	"go/ast"
	"go/build"
	"go/parser"
	"go/token"

	"path/filepath"

	"encoding/json"
	"io/ioutil"

	"github.com/maximilien/i18n4cf/cmds"
	"github.com/maximilien/i18n4cf/common"
)

type ExtractStrings struct {
	options cmds.Options

	i18nFilename string
	poFilename   string

	Filename      string
	OutputDirname string

	ExtractedStrings map[string]common.StringInfo
	FilteredStrings  map[string]string
	FilteredRegexps  []*regexp.Regexp

	TotalStringsDir int
	TotalStrings    int
	TotalFiles      int

	IgnoreRegexp *regexp.Regexp
}

func NewExtractStrings(options cmds.Options) ExtractStrings {
	var compiledRegexp *regexp.Regexp
	if options.IgnoreRegexp != "" {
		compiledReg, err := regexp.Compile(options.IgnoreRegexp)
		if err != nil {
			fmt.Println("WARNING compiling ignore-regexp:", err)
		}
		compiledRegexp = compiledReg
	}

	return ExtractStrings{options: options,
		Filename:         "extracted_strings.json",
		OutputDirname:    options.OutputDirFlag,
		ExtractedStrings: nil,
		FilteredStrings:  nil,
		FilteredRegexps:  nil,
		TotalStringsDir:  0,
		TotalStrings:     0,
		TotalFiles:       0,
		IgnoreRegexp:     compiledRegexp}
}

func (es *ExtractStrings) Options() cmds.Options {
	return es.options
}

func (es *ExtractStrings) Println(a ...interface{}) (int, error) {
	if es.options.VerboseFlag {
		return fmt.Println(a...)
	}

	return 0, nil
}

func (es *ExtractStrings) Printf(msg string, a ...interface{}) (int, error) {
	if es.options.VerboseFlag {
		return fmt.Printf(msg, a...)
	}

	return 0, nil
}

func (es *ExtractStrings) InspectFile(filename string) error {
	es.Println("gi18n: extracting strings from file:", filename)
	if es.options.DryRunFlag {
		es.Println("WARNING running in -dry-run mode")
	}

	es.ExtractedStrings = make(map[string]common.StringInfo)
	es.FilteredStrings = make(map[string]string)
	es.FilteredRegexps = []*regexp.Regexp{}

	es.setFilename(filename)
	es.setI18nFilename(filename)
	es.setPoFilename(filename)

	fset := token.NewFileSet()

	var absFilePath = filename
	if !filepath.IsAbs(absFilePath) {
		absFilePath = filepath.Join(os.Getenv("PWD"), absFilePath)
	}

	fileInfo, err := common.GetAbsFileInfo(absFilePath)
	if err != nil {
		es.Println(err)
	}

	if strings.HasPrefix(fileInfo.Name(), ".") {
		es.Println("WARNING ignoring file:", absFilePath)
		return nil
	}

	astFile, err := parser.ParseFile(fset, absFilePath, nil, parser.ParseComments|parser.AllErrors)
	if err != nil {
		es.Println(err)
		return err
	}

	err = es.loadExcludedStrings()
	if err != nil {
		es.Println(err)
		return err
	}
	es.Println(fmt.Sprintf("Loaded %d excluded strings", len(es.FilteredStrings)))

	err = es.loadExcludedRegexps()
	if err != nil {
		es.Println(err)
		return err
	}
	es.Println(fmt.Sprintf("Loaded %d excluded regexps", len(es.FilteredRegexps)))

	es.excludeImports(astFile)

	es.extractString(astFile, fset)
	es.TotalStringsDir += len(es.ExtractedStrings)
	es.TotalStrings += len(es.ExtractedStrings)
	es.TotalFiles += 1

	es.Printf("Extracted %d strings from file: %s\n", len(es.ExtractedStrings), absFilePath)

	var outputDirname = es.OutputDirname
	if es.options.OutputDirFlag != "" {
		if es.options.OutputMatchImportFlag {
			outputDirname, err = es.findImportPath(absFilePath)
			if err != nil {
				es.Println(err)
				return err
			}
		} else if es.options.OutputMatchPackageFlag {
			outputDirname, err = es.findPackagePath(absFilePath)
			if err != nil {
				es.Println(err)
				return err
			}
		}
	} else {
		outputDirname, err = common.FindFilePath(absFilePath)
		if err != nil {
			es.Println(err)
			return err
		}
	}

	err = es.saveExtractedStrings(outputDirname)
	if err != nil {
		es.Println(err)
		return err
	}

	err = common.SaveStrings(es, es.ExtractedStrings, outputDirname, es.i18nFilename)
	if err != nil {
		es.Println(err)
		return err
	}

	if es.options.PoFlag {
		err = common.SaveStringsInPo(es, es.ExtractedStrings, outputDirname, es.poFilename)
		if err != nil {
			es.Println(err)
			return err
		}
	}

	return nil
}

func (es *ExtractStrings) InspectDir(dirName string, recursive bool) error {
	es.Printf("gi18n: inspecting dir %s, recursive: %t\n", dirName, recursive)
	es.Println()

	fset := token.NewFileSet()
	es.TotalStringsDir = 0

	packages, err := parser.ParseDir(fset, dirName, nil, parser.ParseComments)
	if err != nil {
		es.Println(err)
		return err
	}

	for k, pkg := range packages {
		es.Println("Extracting strings in package:", k)
		for fileName, _ := range pkg.Files {
			if es.IgnoreRegexp != nil && es.IgnoreRegexp.MatchString(fileName) {
				es.Println("Using ignore-regexp:", es.options.IgnoreRegexp)
				continue
			} else {
				es.Println("No match for ignore-regexp:", es.options.IgnoreRegexp)
			}

			if strings.HasSuffix(fileName, ".go") {
				err = es.InspectFile(fileName)
				if err != nil {
					es.Println(err)
				}
			}
		}
	}
	es.Printf("Extracted total of %d strings\n\n", es.TotalStringsDir)

	if recursive {
		fileInfos, _ := ioutil.ReadDir(dirName)
		for _, fileInfo := range fileInfos {
			if fileInfo.IsDir() && !strings.HasPrefix(fileInfo.Name(), ".") {
				err = es.InspectDir(filepath.Join(dirName, fileInfo.Name()), recursive)
				if err != nil {
					es.Println(err)
				}
			}
		}
	}

	return nil
}

func (es *ExtractStrings) findImportPath(filename string) (string, error) {
	path := es.OutputDirname

	filePath, err := common.FindFilePath(filename)
	if err != nil {
		fmt.Println("ERROR opening file", err)
		return "", err
	}

	pkg, err := build.ImportDir(filePath, 0)
	srcPath := "src" + string(os.PathSeparator)
	if strings.HasPrefix(pkg.Dir, srcPath) {
		path = filepath.Join(path, pkg.Dir[len(srcPath):len(pkg.Dir)])
	}

	return path, nil
}

func (es *ExtractStrings) findPackagePath(filename string) (string, error) {
	path := es.OutputDirname

	filePath, err := common.FindFilePath(filename)
	if err != nil {
		fmt.Println("ERROR opening file", err)
		return "", err
	}

	pkg, err := build.ImportDir(filePath, 0)
	if err != nil {
		fmt.Println("ERROR opening file", err)
		return "", err
	}

	return filepath.Join(path, pkg.Name), nil
}

func (es *ExtractStrings) saveExtractedStrings(outputDirname string) error {
	if len(es.ExtractedStrings) != 0 {
		es.Println("Saving extracted strings to file:", es.Filename)
	}

	common.CreateOutputDirsIfNeeded(outputDirname)

	stringInfos := make([]common.StringInfo, 0)
	for _, stringInfo := range es.ExtractedStrings {
		stringInfo.Filename = strings.Split(es.Filename, ".extracted.json")[0]

		stringInfos = append(stringInfos, stringInfo)
	}

	jsonData, err := json.MarshalIndent(stringInfos, "", "   ")
	if err != nil {
		es.Println(err)
		return err
	}

	if !es.options.DryRunFlag && len(stringInfos) != 0 {
		file, err := os.Create(filepath.Join(outputDirname, es.Filename[strings.LastIndex(es.Filename, string(os.PathSeparator))+1:len(es.Filename)]))
		defer file.Close()
		if err != nil {
			es.Println(err)
			return err
		}

		file.Write(jsonData)
	}

	return nil
}

func (es *ExtractStrings) setFilename(filename string) {
	es.Filename = filename + ".extracted.json"
}

func (es *ExtractStrings) setI18nFilename(filename string) {
	es.i18nFilename = filename + ".en.json"
}

func (es *ExtractStrings) setPoFilename(filename string) {
	es.poFilename = filename + ".en.po"
}

func (es *ExtractStrings) loadExcludedStrings() error {
	_, err := os.Stat(es.options.ExcludedFilenameFlag)
	if os.IsNotExist(err) {
		es.Println("Could not find:", es.options.ExcludedFilenameFlag)
		return nil
	}

	es.Println("Excluding strings in file:", es.options.ExcludedFilenameFlag)

	content, err := ioutil.ReadFile(es.options.ExcludedFilenameFlag)
	if err != nil {
		fmt.Print(err)
		return err
	}

	var excludedStrings common.ExcludedStrings
	err = json.Unmarshal(content, &excludedStrings)
	if err != nil {
		fmt.Print(err)
		return err
	}

	for i := range excludedStrings.ExcludedStrings {
		es.FilteredStrings[excludedStrings.ExcludedStrings[i]] = excludedStrings.ExcludedStrings[i]
	}

	return nil
}

func (es *ExtractStrings) loadExcludedRegexps() error {
	_, err := os.Stat(es.options.ExcludedFilenameFlag)
	if os.IsNotExist(err) {
		es.Println("Could not find:", es.options.ExcludedFilenameFlag)
		return nil
	}

	es.Println("Excluding regexps in file:", es.options.ExcludedFilenameFlag)

	content, err := ioutil.ReadFile(es.options.ExcludedFilenameFlag)
	if err != nil {
		fmt.Print(err)
		return err
	}

	var excludedRegexps common.ExcludedStrings
	err = json.Unmarshal(content, &excludedRegexps)
	if err != nil {
		fmt.Print(err)
		return err
	}

	for _, regexpString := range excludedRegexps.ExcludedRegexps {
		compiledRegexp, err := regexp.Compile(regexpString)
		if err != nil {
			fmt.Println("WARNING error compiling regexp:", regexpString)
		}

		es.FilteredRegexps = append(es.FilteredRegexps, compiledRegexp)
	}

	return nil
}

func (es *ExtractStrings) extractString(f *ast.File, fset *token.FileSet) error {
	ast.Inspect(f, func(n ast.Node) bool {
		var s string
		switch x := n.(type) {
		case *ast.BasicLit:
			s, _ = strconv.Unquote(x.Value)
			if len(s) > 0 && x.Kind == token.STRING && s != "\t" && s != "\n" && s != " " && !es.filter(s) { //TODO: fix to remove these: s != "\\t" && s != "\\n" && s != " "
				position := fset.Position(n.Pos())
				stringInfo := common.StringInfo{Value: s,
					Filename: position.Filename,
					Offset:   position.Offset,
					Line:     position.Line,
					Column:   position.Column}
				es.ExtractedStrings[s] = stringInfo
			}
		}
		return true
	})

	return nil
}

func (es *ExtractStrings) excludeImports(astFile *ast.File) {
	for i := range astFile.Imports {
		importString, _ := strconv.Unquote(astFile.Imports[i].Path.Value)
		es.FilteredStrings[importString] = importString
	}

}

func (es *ExtractStrings) filter(aString string) bool {
	for i := range common.BLANKS {
		if aString == common.BLANKS[i] {
			return true
		}
	}

	if es.FilteredStrings[aString] != "" {
		return true
	}

	for _, compiledRegexp := range es.FilteredRegexps {
		if compiledRegexp.MatchString(aString) {
			return true
		}
	}

	return false
}
