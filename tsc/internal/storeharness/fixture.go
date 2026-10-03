package storeharness

import (
	"cmp"
	"encoding/json"
	"fmt"
	"os"
	"reflect"
	"slices"
	"strconv"
	"strings"

	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/ast/store"
	"github.com/microsoft/TypeScript/tsc/internal/collections"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/module"
	"github.com/microsoft/TypeScript/tsc/internal/packagejson"
	"github.com/microsoft/TypeScript/tsc/internal/storebinder"
	"github.com/microsoft/TypeScript/tsc/internal/storechecker"
	"github.com/microsoft/TypeScript/tsc/internal/storeparser"
	"github.com/microsoft/TypeScript/tsc/internal/symlinks"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
)

// The measurement program of store-checker-7d2c-implementation-instructions-
// 20261002.md 3: a Fixture holds, as values only, what the Store checker
// asked of an H1 Program (adapter.go) in a run, and FixtureProgram answers
// the same questions from it and the Store files parsed from its texts, with
// no Pointer program, parser, source file or lib cache behind it. A question
// the recording did not see panics: there is no fallback.
//
// A Recorder is the H1 Program with every answer written down. The answers
// are recorded by their arguments as values: a file by its path, a node by
// its file, position, end and kind. Answers that are pointers keep their
// identity: equal pointers are one entry of a table. The few answers that are
// Pointer structures (a ParsedCommandLine, a project reference) are recorded
// only when nil; any other value panics in the recording, since the fixture
// cannot hold it. The symlink cache is its maps, as values, taken when it is
// first asked: the checker only reads it. A package.json entry is the text of
// its file, parsed again on the first question (the resolver's packagejson.Parse).

// Fixture is the recorded program. Its JSON is the file between the
// preparation and the measurement processes.
type Fixture struct {
	Files   []FixtureFile
	Options *core.CompilerOptions
	Modules []fixtureModule // the distinct resolved modules, by identity, in the order of sortModules
	// DroppedResolutionDiagnostics counts the modules' resolution diagnostics,
	// which are Pointer diagnostics and which no Store package reads.
	DroppedResolutionDiagnostics int

	Scalars        fixtureScalars
	SourceFile     map[string]int   // GetSourceFile: file index or -1
	ForResolved    map[string]int   // GetSourceFileForResolvedModule
	FileExists     map[string]bool  // FileExists
	MayBeEmitted   map[string]bool  // SourceFileMayBeEmitted: "path|force"
	EmitFormat     map[string]int32 // GetEmitModuleFormatOfFile: path
	ImpliedFormat  map[string]int32 // GetImpliedNodeFormatForEmit: path
	EmitSyntax     map[string]int32 // GetEmitSyntaxForUsageLocation: node key
	UsageMode      map[string]int32 // GetModeForUsageLocation: node key
	DefaultMode    map[string]int32 // GetDefaultResolutionModeForFile: path
	Resolved       map[string]int   // GetResolvedModule: "path|reference|mode", module index or -1
	FromSpecifier  map[string]int   // GetResolvedModuleFromModuleSpecifier: node key
	MetaData       map[string]ast.SourceFileMetaData
	DefaultLibrary map[string]bool
	NilRedirect    map[string]bool                // GetRedirectForResolution: path, nil only
	NilOutputDts   map[string]bool                // GetProjectReferenceFromOutputDts
	NilFromSource  map[string]bool                // GetProjectReferenceFromSource
	PackageJson    map[string]*fixturePackageJson // GetPackageJsonInfo: nil for nil
	RedirectTarget map[string][]string
	SourceOfOutput map[string]string // GetSourceOfProjectReferenceIfOutputIncluded: path
	NearestPackage map[string]string // GetNearestAncestorDirectoryWithPackageJson
	JSXReference   map[string]string // GetJSXRuntimeImportSpecifier's module reference: path
}

// FixtureFile is a source file of the program, in its order, as the Store
// parser takes it, and its synthetic imports' texts ("" for none).
type FixtureFile struct {
	ParseOptions ast.SourceFileParseOptions
	Text         []byte // bytes: JSON's strings replace invalid UTF-8
	ScriptKind   core.ScriptKind
	HelperImport string
	JSXImport    string
}

type fixtureModule struct {
	ResolvedFileName             string
	OriginalPath                 string
	Extension                    string
	ResolvedUsingTsExtension     bool
	ResolvedUsingExtraExtensions bool
	PackageId                    module.PackageId
	IsExternalLibraryImport      bool
	AlternateResult              string
}

type fixtureScalars struct {
	Asked                     map[string]bool // which of the scalars were asked
	PackagesMap               map[string]bool
	ResolvedModulesNil        bool
	CommonSourceDirectory     string
	ContentMapperExtensions   []string
	GlobalTypingsLocation     string
	UseCaseSensitiveFileNames bool
	CurrentDirectory          string
	Symlinks                  *fixtureSymlinks
}

// fixtureSymlinks is a KnownSymlinks as values: its four maps.
type fixtureSymlinks struct {
	CurrentDirectory          string
	UseCaseSensitiveFileNames bool
	Directories               map[tspath.Path]*symlinks.KnownDirectoryLink
	DirectoriesByRealpath     map[tspath.Path][]string
	Files                     map[tspath.Path]string
	FilesByRealpath           map[tspath.Path][]string
}

func symlinksValue(cache *symlinks.KnownSymlinks, cwd string, useCaseSensitiveFileNames bool) *fixtureSymlinks {
	v := &fixtureSymlinks{
		CurrentDirectory: cwd, UseCaseSensitiveFileNames: useCaseSensitiveFileNames,
		Directories: map[tspath.Path]*symlinks.KnownDirectoryLink{}, DirectoriesByRealpath: map[tspath.Path][]string{},
		Files: map[tspath.Path]string{}, FilesByRealpath: map[tspath.Path][]string{},
	}
	cache.Directories().Range(func(k tspath.Path, link *symlinks.KnownDirectoryLink) bool {
		if link != nil {
			link = &symlinks.KnownDirectoryLink{Real: link.Real, RealPath: link.RealPath}
		}
		v.Directories[k] = link
		return true
	})
	cache.Files().Range(func(k tspath.Path, real string) bool { v.Files[k] = real; return true })
	sets := func(m *collections.SyncMap[tspath.Path, *collections.SyncSet[string]], into map[tspath.Path][]string) {
		m.Range(func(k tspath.Path, set *collections.SyncSet[string]) bool {
			into[k] = []string{}
			set.Range(func(s string) bool { into[k] = append(into[k], s); return true })
			return true
		})
	}
	sets(cache.DirectoriesByRealpath(), v.DirectoriesByRealpath)
	sets(cache.FilesByRealpath(), v.FilesByRealpath)
	return v
}

func (v *fixtureSymlinks) cache() *symlinks.KnownSymlinks {
	cache := symlinks.NewKnownSymlink(v.CurrentDirectory, v.UseCaseSensitiveFileNames)
	for k, link := range v.Directories {
		cache.Directories().Store(k, link)
	}
	for k, real := range v.Files {
		cache.Files().Store(k, real)
	}
	sets := func(from map[tspath.Path][]string, m *collections.SyncMap[tspath.Path, *collections.SyncSet[string]]) {
		for k, items := range from {
			set := &collections.SyncSet[string]{}
			for _, s := range items {
				set.Add(s)
			}
			m.Store(k, set)
		}
	}
	sets(v.DirectoriesByRealpath, cache.DirectoriesByRealpath())
	sets(v.FilesByRealpath, cache.FilesByRealpath())
	return cache
}

func nodeKey(file ast.HasFileName, node store.Node) string {
	return fmt.Sprintf("%s|%d|%d|%d", file.Path(), node.Pos(), node.End(), node.Kind())
}

func pathKey(file ast.HasFileName) string { return string(file.Path()) }

// nodeKeyOf is nodeKey as a map key, for FixtureProgram's lookups: making the
// string of nodeKey on every call cost more than the lookup
// (store-checker-7d2c-abc-implementation-instructions-20261002.md 3).
// The file is the program's Store file of the key's path: hashing the path
// was most of the lookup.
type nodeKeyOf struct {
	file     *store.File
	pos, end int32
	kind     ast.Kind
}

func (p *FixtureProgram) nodeKeyFor(file ast.HasFileName, node store.Node) nodeKeyOf {
	f, ok := file.(*store.File)
	if !ok {
		f = p.byPath[string(file.Path())]
	}
	return nodeKeyOf{f, node.Pos(), node.End(), node.Kind()}
}

// byNodeKey makes a map of nodeKey strings into one of nodeKeyOf. Run it once
// byPath is complete.
func byNodeKey[V any](p *FixtureProgram, m map[string]V) map[nodeKeyOf]V {
	out := make(map[nodeKeyOf]V, len(m))
	for k, v := range m {
		rest, kind, ok1 := cutLast(k)
		rest, end, ok2 := cutLast(rest)
		path, pos, ok3 := cutLast(rest)
		if !ok1 || !ok2 || !ok3 {
			panic("fixture: bad node key " + k)
		}
		f := p.byPath[path]
		if f == nil {
			panic("fixture: node key of a file not in the program " + k)
		}
		out[nodeKeyOf{f, int32(pos), int32(end), ast.Kind(kind)}] = v
	}
	return out
}

// cutLast splits "s|n" into s and n.
func cutLast(k string) (string, int, bool) {
	i := strings.LastIndexByte(k, '|')
	if i < 0 {
		return "", 0, false
	}
	n, err := strconv.Atoi(k[i+1:])
	return k[:i], n, err == nil
}

// WriteFixture writes f as JSON to name.
func WriteFixture(f *Fixture, name string) error {
	data, err := json.Marshal(f)
	if err != nil {
		return err
	}
	return os.WriteFile(name, data, 0o644)
}

// ReadFixture reads a fixture WriteFixture wrote.
func ReadFixture(name string) (*Fixture, error) {
	data, err := os.ReadFile(name)
	if err != nil {
		return nil, err
	}
	f := &Fixture{}
	if err := json.Unmarshal(data, f); err != nil {
		return nil, err
	}
	return f, nil
}

// Recorder is an H1 Program that records its answers into a Fixture.
type Recorder struct {
	*Program
	fixture *Fixture
	index   map[*store.File]int
	modules map[*module.ResolvedModule]int
}

var _ storechecker.Program = (*Recorder)(nil)

// NewRecorder records the answers of program. The files, their texts and the
// options are recorded now; the answers as they are asked.
func NewRecorder(program *Program) *Recorder {
	f := &Fixture{
		Options:        program.pointer.Options(),
		Scalars:        fixtureScalars{Asked: map[string]bool{}},
		SourceFile:     map[string]int{},
		ForResolved:    map[string]int{},
		FileExists:     map[string]bool{},
		MayBeEmitted:   map[string]bool{},
		EmitFormat:     map[string]int32{},
		ImpliedFormat:  map[string]int32{},
		EmitSyntax:     map[string]int32{},
		UsageMode:      map[string]int32{},
		DefaultMode:    map[string]int32{},
		Resolved:       map[string]int{},
		FromSpecifier:  map[string]int{},
		MetaData:       map[string]ast.SourceFileMetaData{},
		DefaultLibrary: map[string]bool{},
		NilRedirect:    map[string]bool{},
		NilOutputDts:   map[string]bool{},
		NilFromSource:  map[string]bool{},
		PackageJson:    map[string]*fixturePackageJson{},
		RedirectTarget: map[string][]string{},
		SourceOfOutput: map[string]string{},
		NearestPackage: map[string]string{},
		JSXReference:   map[string]string{},
	}
	r := &Recorder{Program: program, fixture: f, index: map[*store.File]int{}, modules: map[*module.ResolvedModule]int{}}
	for i, sf := range program.files {
		pf := program.pointers[sf.Path()]
		ff := FixtureFile{ParseOptions: pf.ParseOptions(), Text: []byte(pf.Text()), ScriptKind: pf.ScriptKind}
		if s := program.helperImports[sf.Path()]; !s.IsNil() {
			ff.HelperImport = s.Text()
		}
		if s := program.jsxImports[sf.Path()]; !s.IsNil() {
			ff.JSXImport = s.Text()
		}
		f.Files = append(f.Files, ff)
		r.index[sf] = i
	}
	return r
}

// Fixture is what r has recorded, its options checked to survive JSON and its
// modules sorted.
func (r *Recorder) Fixture() (*Fixture, error) {
	data, err := json.Marshal(r.fixture.Options)
	if err != nil {
		return nil, err
	}
	var back core.CompilerOptions
	if err := json.Unmarshal(data, &back); err != nil {
		return nil, err
	}
	if !reflect.DeepEqual(&back, r.fixture.Options) {
		return nil, fmt.Errorf("the options do not survive JSON")
	}
	if err := r.sortModules(); err != nil {
		return nil, err
	}
	return r.fixture, nil
}

// sortModules numbers the modules in the order of their values (their JSON),
// then of the least key that refers to each, since two distinct modules can be
// equal, and renumbers Resolved, FromSpecifier and r.modules. moduleIndex
// numbers them in the order the checker first asks them, which is not the same
// in every run, and the fixture's bytes would follow it
// (store-checker-7d2d-implementation-instructions-20261003.md 3).
func (r *Recorder) sortModules() error {
	f := r.fixture
	values := make([]string, len(f.Modules))
	for i, m := range f.Modules {
		data, err := json.Marshal(m)
		if err != nil {
			return err
		}
		values[i] = string(data)
	}
	least := make([]string, len(f.Modules))
	for _, refs := range []struct {
		prefix string
		m      map[string]int
	}{{"Resolved ", f.Resolved}, {"FromSpecifier ", f.FromSpecifier}} {
		for k, i := range refs.m {
			if i >= 0 && (least[i] == "" || refs.prefix+k < least[i]) {
				least[i] = refs.prefix + k
			}
		}
	}
	order := make([]int, len(f.Modules)) // old index by new
	for i := range order {
		order[i] = i
	}
	slices.SortFunc(order, func(a, b int) int {
		return cmp.Or(strings.Compare(values[a], values[b]), strings.Compare(least[a], least[b]))
	})
	renumber := make([]int, len(f.Modules)) // new index by old
	modules := make([]fixtureModule, len(f.Modules))
	for to, from := range order {
		renumber[from] = to
		modules[to] = f.Modules[from]
	}
	f.Modules = modules
	for _, m := range []map[string]int{f.Resolved, f.FromSpecifier} {
		for k, i := range m {
			if i >= 0 {
				m[k] = renumber[i]
			}
		}
	}
	for m, i := range r.modules {
		r.modules[m] = renumber[i]
	}
	return nil
}

func (r *Recorder) fileIndex(f *store.File) int {
	if f == nil {
		return -1
	}
	return r.index[f]
}

func (r *Recorder) moduleIndex(m *module.ResolvedModule) int {
	if m == nil {
		return -1
	}
	if i, ok := r.modules[m]; ok {
		return i
	}
	r.fixture.DroppedResolutionDiagnostics += len(m.ResolutionDiagnostics)
	i := len(r.fixture.Modules)
	r.modules[m] = i
	r.fixture.Modules = append(r.fixture.Modules, fixtureModule{m.ResolvedFileName, m.OriginalPath, m.Extension, m.ResolvedUsingTsExtension, m.ResolvedUsingExtraExtensions, m.PackageId, m.IsExternalLibraryImport, m.AlternateResult})
	return i
}

// fixturePackageJson is a package.json entry as values: the text of the file,
// parsed again by packagejson.Parse, as the resolver parses it.
type fixturePackageJson struct {
	PackageDirectory string
	DirectoryExists  bool
	HasContents      bool
	Text             []byte
}

func (r *Recorder) packageJsonValue(path string, v *packagejson.InfoCacheEntry) *fixturePackageJson {
	if v == nil {
		return nil
	}
	value := &fixturePackageJson{PackageDirectory: v.PackageDirectory, DirectoryExists: v.DirectoryExists}
	if v.Contents != nil {
		text, ok := r.pointer.Host().FS().ReadFile(path)
		if !ok {
			panic("fixture: package.json " + path + " cannot be read")
		}
		value.HasContents, value.Text = true, []byte(text)
		if again := value.entry(); !reflect.DeepEqual(again.Contents.Fields, v.Contents.Fields) || again.Contents.Parseable != v.Contents.Parseable {
			panic("fixture: package.json " + path + " does not parse to the program's")
		}
	}
	return value
}

func (v *fixturePackageJson) entry() *packagejson.InfoCacheEntry {
	e := &packagejson.InfoCacheEntry{PackageDirectory: v.PackageDirectory, DirectoryExists: v.DirectoryExists}
	if v.HasContents {
		fields, err := packagejson.Parse(v.Text)
		e.Contents = &packagejson.PackageJson{Fields: fields, Parseable: err == nil}
	}
	return e
}

func mustBeNil(what string, isNil bool) {
	if !isNil {
		panic("fixture: " + what + " is not nil")
	}
}

func (r *Recorder) GetSourceFile(fileName string) *store.File {
	f := r.Program.GetSourceFile(fileName)
	r.fixture.SourceFile[fileName] = r.fileIndex(f)
	return f
}

func (r *Recorder) GetSourceFileForResolvedModule(fileName string) *store.File {
	f := r.Program.GetSourceFileForResolvedModule(fileName)
	r.fixture.ForResolved[fileName] = r.fileIndex(f)
	return f
}

func (r *Recorder) FileExists(fileName string) bool {
	v := r.Program.FileExists(fileName)
	r.fixture.FileExists[fileName] = v
	return v
}

func (r *Recorder) SourceFileMayBeEmitted(sourceFile *store.File, forceDtsEmit bool) bool {
	v := r.Program.SourceFileMayBeEmitted(sourceFile, forceDtsEmit)
	r.fixture.MayBeEmitted[fmt.Sprintf("%s|%t", sourceFile.Path(), forceDtsEmit)] = v
	return v
}

func (r *Recorder) GetEmitModuleFormatOfFile(sourceFile ast.HasFileName) core.ModuleKind {
	v := r.Program.GetEmitModuleFormatOfFile(sourceFile)
	r.fixture.EmitFormat[pathKey(sourceFile)] = int32(v)
	return v
}

func (r *Recorder) GetImpliedNodeFormatForEmit(sourceFile ast.HasFileName) core.ModuleKind {
	v := r.Program.GetImpliedNodeFormatForEmit(sourceFile)
	r.fixture.ImpliedFormat[pathKey(sourceFile)] = int32(v)
	return v
}

func (r *Recorder) GetEmitSyntaxForUsageLocation(file ast.HasFileName, usage store.Node) core.ResolutionMode {
	v := r.Program.GetEmitSyntaxForUsageLocation(file, usage)
	r.fixture.EmitSyntax[nodeKey(file, usage)] = int32(v)
	return v
}

func (r *Recorder) GetModeForUsageLocation(file ast.HasFileName, usage store.Node) core.ResolutionMode {
	v := r.Program.GetModeForUsageLocation(file, usage)
	r.fixture.UsageMode[nodeKey(file, usage)] = int32(v)
	return v
}

func (r *Recorder) GetDefaultResolutionModeForFile(file ast.HasFileName) core.ResolutionMode {
	v := r.Program.GetDefaultResolutionModeForFile(file)
	r.fixture.DefaultMode[pathKey(file)] = int32(v)
	return v
}

func (r *Recorder) GetResolvedModule(file ast.HasFileName, moduleReference string, mode core.ResolutionMode) *module.ResolvedModule {
	m := r.Program.GetResolvedModule(file, moduleReference, mode)
	r.fixture.Resolved[fmt.Sprintf("%s|%s|%d", file.Path(), moduleReference, mode)] = r.moduleIndex(m)
	return m
}

func (r *Recorder) GetResolvedModuleFromModuleSpecifier(file ast.HasFileName, moduleSpecifier store.Node) *module.ResolvedModule {
	m := r.Program.GetResolvedModuleFromModuleSpecifier(file, moduleSpecifier)
	r.fixture.FromSpecifier[nodeKey(file, moduleSpecifier)] = r.moduleIndex(m)
	return m
}

func (r *Recorder) GetResolvedModules() map[tspath.Path]module.ModeAwareCache[*module.ResolvedModule] {
	v := r.Program.GetResolvedModules()
	mustBeNil("GetResolvedModules", v == nil)
	r.fixture.Scalars.Asked["ResolvedModules"] = true
	r.fixture.Scalars.ResolvedModulesNil = true
	return v
}

func (r *Recorder) GetPackagesMap() map[string]bool {
	v := r.Program.GetPackagesMap()
	r.fixture.Scalars.Asked["PackagesMap"] = true
	r.fixture.Scalars.PackagesMap = v
	return v
}

func (r *Recorder) GetSourceFileMetaData(path tspath.Path) ast.SourceFileMetaData {
	v := r.Program.GetSourceFileMetaData(path)
	r.fixture.MetaData[string(path)] = v
	return v
}

func (r *Recorder) IsSourceFileDefaultLibrary(path tspath.Path) bool {
	v := r.Program.IsSourceFileDefaultLibrary(path)
	r.fixture.DefaultLibrary[string(path)] = v
	return v
}

func (r *Recorder) GetRedirectForResolution(file ast.HasFileName) *tsoptions.ParsedCommandLine {
	v := r.Program.GetRedirectForResolution(file)
	mustBeNil("GetRedirectForResolution", v == nil)
	r.fixture.NilRedirect[pathKey(file)] = true
	return v
}

func (r *Recorder) GetProjectReferenceFromOutputDts(path tspath.Path) *tsoptions.SourceOutputAndProjectReference {
	v := r.Program.GetProjectReferenceFromOutputDts(path)
	mustBeNil("GetProjectReferenceFromOutputDts", v == nil)
	r.fixture.NilOutputDts[string(path)] = true
	return v
}

func (r *Recorder) GetProjectReferenceFromSource(path tspath.Path) *tsoptions.SourceOutputAndProjectReference {
	v := r.Program.GetProjectReferenceFromSource(path)
	mustBeNil("GetProjectReferenceFromSource", v == nil)
	r.fixture.NilFromSource[string(path)] = true
	return v
}

func (r *Recorder) GetPackageJsonInfo(pkgJsonPath string) *packagejson.InfoCacheEntry {
	v := r.Program.GetPackageJsonInfo(pkgJsonPath)
	if _, ok := r.fixture.PackageJson[pkgJsonPath]; !ok {
		r.fixture.PackageJson[pkgJsonPath] = r.packageJsonValue(pkgJsonPath, v)
	}
	return v
}

func (r *Recorder) GetSymlinkCache() *symlinks.KnownSymlinks {
	v := r.Program.GetSymlinkCache()
	if !r.fixture.Scalars.Asked["SymlinkCache"] {
		r.fixture.Scalars.Asked["SymlinkCache"] = true
		if v != nil {
			r.fixture.Scalars.Symlinks = symlinksValue(v, r.Program.GetCurrentDirectory(), r.Program.UseCaseSensitiveFileNames())
		}
	}
	return v
}

func (r *Recorder) GetRedirectTargets(path tspath.Path) []string {
	v := r.Program.GetRedirectTargets(path)
	r.fixture.RedirectTarget[string(path)] = v
	return v
}

func (r *Recorder) GetSourceOfProjectReferenceIfOutputIncluded(file ast.HasFileName) string {
	v := r.Program.GetSourceOfProjectReferenceIfOutputIncluded(file)
	r.fixture.SourceOfOutput[pathKey(file)] = v
	return v
}

func (r *Recorder) GetNearestAncestorDirectoryWithPackageJson(dirname string) string {
	v := r.Program.GetNearestAncestorDirectoryWithPackageJson(dirname)
	r.fixture.NearestPackage[dirname] = v
	return v
}

func (r *Recorder) GetJSXRuntimeImportSpecifier(path tspath.Path) (string, store.Node) {
	reference, specifier := r.Program.GetJSXRuntimeImportSpecifier(path)
	r.fixture.JSXReference[string(path)] = reference
	return reference, specifier
}

func (r *Recorder) CommonSourceDirectory() string {
	v := r.Program.CommonSourceDirectory()
	r.fixture.Scalars.Asked["CommonSourceDirectory"] = true
	r.fixture.Scalars.CommonSourceDirectory = v
	return v
}

func (r *Recorder) ContentMapperExtensions() []string {
	v := r.Program.ContentMapperExtensions()
	r.fixture.Scalars.Asked["ContentMapperExtensions"] = true
	r.fixture.Scalars.ContentMapperExtensions = v
	return v
}

func (r *Recorder) GetGlobalTypingsCacheLocation() string {
	v := r.Program.GetGlobalTypingsCacheLocation()
	r.fixture.Scalars.Asked["GlobalTypingsLocation"] = true
	r.fixture.Scalars.GlobalTypingsLocation = v
	return v
}

func (r *Recorder) UseCaseSensitiveFileNames() bool {
	v := r.Program.UseCaseSensitiveFileNames()
	r.fixture.Scalars.Asked["UseCaseSensitiveFileNames"] = true
	r.fixture.Scalars.UseCaseSensitiveFileNames = v
	return v
}

func (r *Recorder) GetCurrentDirectory() string {
	v := r.Program.GetCurrentDirectory()
	r.fixture.Scalars.Asked["CurrentDirectory"] = true
	r.fixture.Scalars.CurrentDirectory = v
	return v
}

// FixtureProgram is the measurement program: a Fixture and the Store files of
// its texts. Calls counts the calls of each method (9's 1: its methods cost
// what a map lookup costs, not what a production program's do).
type FixtureProgram struct {
	fixture       *Fixture
	space         *store.IDSpace
	files         []*store.File
	byPath        map[string]*store.File
	modules       []*module.ResolvedModule
	helperImports map[tspath.Path]store.Node
	jsxImports    map[tspath.Path]store.Node
	symlinks      *symlinks.KnownSymlinks
	packageJsons  map[string]*packagejson.InfoCacheEntry // one entry for a path, as the program's cache
	emitSyntax    map[nodeKeyOf]int32
	usageMode     map[nodeKeyOf]int32
	fromSpecifier map[nodeKeyOf]int
	Calls         [numCalls]int // by call; CallCounts names them
}

// call names a method of FixtureProgram for Calls: a map of the names cost
// more than the lookups they count
// (store-checker-7d2c-abc-implementation-instructions-20261002.md 3.1).
type call int

const (
	callGetSourceFile call = iota
	callGetSourceFileForResolvedModule
	callFileExists
	callSourceFileMayBeEmitted
	callGetEmitModuleFormatOfFile
	callGetImpliedNodeFormatForEmit
	callGetEmitSyntaxForUsageLocation
	callGetModeForUsageLocation
	callGetDefaultResolutionModeForFile
	callGetResolvedModule
	callGetResolvedModuleFromModuleSpecifier
	callGetSourceFileMetaData
	callIsSourceFileDefaultLibrary
	callGetRedirectForResolution
	callGetProjectReferenceFromOutputDts
	callGetProjectReferenceFromSource
	callGetPackageJsonInfo
	callGetRedirectTargets
	callGetSourceOfProjectReferenceIfOutputIncluded
	callGetNearestAncestorDirectoryWithPackageJson
	callGetJSXRuntimeImportSpecifier
	callResolvedModules
	callPackagesMap
	callSymlinkCache
	callCommonSourceDirectory
	callContentMapperExtensions
	callGlobalTypingsLocation
	callUseCaseSensitiveFileNames
	callCurrentDirectory
	callGetImportHelpersImportSpecifier
	numCalls
)

var callNames = [numCalls]string{
	callGetSourceFile:                               "GetSourceFile",
	callGetSourceFileForResolvedModule:              "GetSourceFileForResolvedModule",
	callFileExists:                                  "FileExists",
	callSourceFileMayBeEmitted:                      "SourceFileMayBeEmitted",
	callGetEmitModuleFormatOfFile:                   "GetEmitModuleFormatOfFile",
	callGetImpliedNodeFormatForEmit:                 "GetImpliedNodeFormatForEmit",
	callGetEmitSyntaxForUsageLocation:               "GetEmitSyntaxForUsageLocation",
	callGetModeForUsageLocation:                     "GetModeForUsageLocation",
	callGetDefaultResolutionModeForFile:             "GetDefaultResolutionModeForFile",
	callGetResolvedModule:                           "GetResolvedModule",
	callGetResolvedModuleFromModuleSpecifier:        "GetResolvedModuleFromModuleSpecifier",
	callGetSourceFileMetaData:                       "GetSourceFileMetaData",
	callIsSourceFileDefaultLibrary:                  "IsSourceFileDefaultLibrary",
	callGetRedirectForResolution:                    "GetRedirectForResolution",
	callGetProjectReferenceFromOutputDts:            "GetProjectReferenceFromOutputDts",
	callGetProjectReferenceFromSource:               "GetProjectReferenceFromSource",
	callGetPackageJsonInfo:                          "GetPackageJsonInfo",
	callGetRedirectTargets:                          "GetRedirectTargets",
	callGetSourceOfProjectReferenceIfOutputIncluded: "GetSourceOfProjectReferenceIfOutputIncluded",
	callGetNearestAncestorDirectoryWithPackageJson:  "GetNearestAncestorDirectoryWithPackageJson",
	callGetJSXRuntimeImportSpecifier:                "GetJSXRuntimeImportSpecifier",
	callResolvedModules:                             "ResolvedModules",
	callPackagesMap:                                 "PackagesMap",
	callSymlinkCache:                                "SymlinkCache",
	callCommonSourceDirectory:                       "CommonSourceDirectory",
	callContentMapperExtensions:                     "ContentMapperExtensions",
	callGlobalTypingsLocation:                       "GlobalTypingsLocation",
	callUseCaseSensitiveFileNames:                   "UseCaseSensitiveFileNames",
	callCurrentDirectory:                            "CurrentDirectory",
	callGetImportHelpersImportSpecifier:             "GetImportHelpersImportSpecifier",
}

// CallCounts is Calls by method name, without the methods not called.
func (p *FixtureProgram) CallCounts() map[string]int {
	m := map[string]int{}
	for c, n := range p.Calls {
		if n != 0 {
			m[callNames[c]] = n
		}
	}
	return m
}

var _ storechecker.Program = (*FixtureProgram)(nil)

// NewFixtureProgram parses the fixture's texts into Store files, each with
// its StoreID in a new IDSpace, in its order, and makes the synthetic imports.
// The files are not bound: BindSourceFiles binds them.
func NewFixtureProgram(f *Fixture) *FixtureProgram {
	p := &FixtureProgram{
		fixture:       f,
		space:         store.NewIDSpace(len(f.Files)),
		files:         make([]*store.File, len(f.Files)),
		byPath:        make(map[string]*store.File, len(f.Files)),
		helperImports: map[tspath.Path]store.Node{},
		jsxImports:    map[tspath.Path]store.Node{},
		packageJsons:  map[string]*packagejson.InfoCacheEntry{},
	}
	for i, ff := range f.Files {
		sf := storeparser.ParseSourceFileIn(p.space, store.StoreID(i), ff.ParseOptions, string(ff.Text), ff.ScriptKind)
		p.files[i] = sf
		p.byPath[string(sf.Path())] = sf
	}
	p.emitSyntax = byNodeKey(p, f.EmitSyntax)
	p.usageMode = byNodeKey(p, f.UsageMode)
	p.fromSpecifier = byNodeKey(p, f.FromSpecifier)
	for i, ff := range f.Files {
		if ff.HelperImport != "" {
			p.helperImports[p.files[i].Path()] = store.NewSyntheticImport(p.files[i], ff.HelperImport)
		}
		if ff.JSXImport != "" {
			p.jsxImports[p.files[i].Path()] = store.NewSyntheticImport(p.files[i], ff.JSXImport)
		}
	}
	if f.Scalars.Symlinks != nil {
		p.symlinks = f.Scalars.Symlinks.cache()
	}
	for _, m := range f.Modules {
		p.modules = append(p.modules, &module.ResolvedModule{
			ResolvedFileName: m.ResolvedFileName, OriginalPath: m.OriginalPath, Extension: m.Extension,
			ResolvedUsingTsExtension: m.ResolvedUsingTsExtension, ResolvedUsingExtraExtensions: m.ResolvedUsingExtraExtensions,
			PackageId: m.PackageId, IsExternalLibraryImport: m.IsExternalLibraryImport, AlternateResult: m.AlternateResult,
		})
	}
	return p
}

// File is the Store file of a path of the program, or nil.
func (p *FixtureProgram) File(path tspath.Path) *store.File { return p.byPath[string(path)] }

func lookup[V any](p *FixtureProgram, method call, m map[string]V, key string) V {
	p.Calls[method]++
	v, ok := m[key]
	if !ok {
		panic(fmt.Sprintf("fixture: %s(%s) was not recorded", callNames[method], key))
	}
	return v
}

// lookupNode is lookup with a nodeKeyOf; the message has the nodeKey string.
func lookupNode[V any](p *FixtureProgram, method call, m map[nodeKeyOf]V, file ast.HasFileName, node store.Node) V {
	p.Calls[method]++
	v, ok := m[p.nodeKeyFor(file, node)]
	if !ok {
		panic(fmt.Sprintf("fixture: %s(%s) was not recorded", callNames[method], nodeKey(file, node)))
	}
	return v
}

func (p *FixtureProgram) scalar(name call) {
	p.Calls[name]++
	if !p.fixture.Scalars.Asked[callNames[name]] {
		panic("fixture: " + callNames[name] + " was not recorded")
	}
}

func (p *FixtureProgram) file(i int) *store.File {
	if i < 0 {
		return nil
	}
	return p.files[i]
}

func (p *FixtureProgram) module(i int) *module.ResolvedModule {
	if i < 0 {
		return nil
	}
	return p.modules[i]
}

func (p *FixtureProgram) SourceFiles() []*store.File     { return p.files }
func (p *FixtureProgram) IDSpace() *store.IDSpace        { return p.space }
func (p *FixtureProgram) Options() *core.CompilerOptions { return p.fixture.Options }

func (p *FixtureProgram) BindSourceFiles() {
	for _, f := range p.files {
		storebinder.BindSourceFile(f)
	}
}

func (p *FixtureProgram) GetSourceFile(fileName string) *store.File {
	return p.file(lookup(p, callGetSourceFile, p.fixture.SourceFile, fileName))
}

func (p *FixtureProgram) GetSourceFileForResolvedModule(fileName string) *store.File {
	return p.file(lookup(p, callGetSourceFileForResolvedModule, p.fixture.ForResolved, fileName))
}

func (p *FixtureProgram) FileExists(fileName string) bool {
	return lookup(p, callFileExists, p.fixture.FileExists, fileName)
}

func (p *FixtureProgram) SourceFileMayBeEmitted(sourceFile *store.File, forceDtsEmit bool) bool {
	return lookup(p, callSourceFileMayBeEmitted, p.fixture.MayBeEmitted, fmt.Sprintf("%s|%t", sourceFile.Path(), forceDtsEmit))
}

func (p *FixtureProgram) GetEmitModuleFormatOfFile(sourceFile ast.HasFileName) core.ModuleKind {
	return core.ModuleKind(lookup(p, callGetEmitModuleFormatOfFile, p.fixture.EmitFormat, pathKey(sourceFile)))
}

func (p *FixtureProgram) GetImpliedNodeFormatForEmit(sourceFile ast.HasFileName) core.ModuleKind {
	return core.ModuleKind(lookup(p, callGetImpliedNodeFormatForEmit, p.fixture.ImpliedFormat, pathKey(sourceFile)))
}

func (p *FixtureProgram) GetEmitSyntaxForUsageLocation(file ast.HasFileName, usage store.Node) core.ResolutionMode {
	return core.ResolutionMode(lookupNode(p, callGetEmitSyntaxForUsageLocation, p.emitSyntax, file, usage))
}

func (p *FixtureProgram) GetModeForUsageLocation(file ast.HasFileName, usage store.Node) core.ResolutionMode {
	return core.ResolutionMode(lookupNode(p, callGetModeForUsageLocation, p.usageMode, file, usage))
}

func (p *FixtureProgram) GetDefaultResolutionModeForFile(file ast.HasFileName) core.ResolutionMode {
	return core.ResolutionMode(lookup(p, callGetDefaultResolutionModeForFile, p.fixture.DefaultMode, pathKey(file)))
}

func (p *FixtureProgram) GetResolvedModule(file ast.HasFileName, moduleReference string, mode core.ResolutionMode) *module.ResolvedModule {
	return p.module(lookup(p, callGetResolvedModule, p.fixture.Resolved, fmt.Sprintf("%s|%s|%d", file.Path(), moduleReference, mode)))
}

func (p *FixtureProgram) GetResolvedModuleFromModuleSpecifier(file ast.HasFileName, moduleSpecifier store.Node) *module.ResolvedModule {
	return p.module(lookupNode(p, callGetResolvedModuleFromModuleSpecifier, p.fromSpecifier, file, moduleSpecifier))
}

func (p *FixtureProgram) GetResolvedModules() map[tspath.Path]module.ModeAwareCache[*module.ResolvedModule] {
	p.scalar(callResolvedModules)
	return nil
}

func (p *FixtureProgram) GetPackagesMap() map[string]bool {
	p.scalar(callPackagesMap)
	return p.fixture.Scalars.PackagesMap
}

func (p *FixtureProgram) GetSourceFileMetaData(path tspath.Path) ast.SourceFileMetaData {
	return lookup(p, callGetSourceFileMetaData, p.fixture.MetaData, string(path))
}

func (p *FixtureProgram) IsSourceFileDefaultLibrary(path tspath.Path) bool {
	return lookup(p, callIsSourceFileDefaultLibrary, p.fixture.DefaultLibrary, string(path))
}

func (p *FixtureProgram) GetRedirectForResolution(file ast.HasFileName) *tsoptions.ParsedCommandLine {
	lookup(p, callGetRedirectForResolution, p.fixture.NilRedirect, pathKey(file))
	return nil
}

func (p *FixtureProgram) GetProjectReferenceFromOutputDts(path tspath.Path) *tsoptions.SourceOutputAndProjectReference {
	lookup(p, callGetProjectReferenceFromOutputDts, p.fixture.NilOutputDts, string(path))
	return nil
}

func (p *FixtureProgram) GetProjectReferenceFromSource(path tspath.Path) *tsoptions.SourceOutputAndProjectReference {
	lookup(p, callGetProjectReferenceFromSource, p.fixture.NilFromSource, string(path))
	return nil
}

func (p *FixtureProgram) GetPackageJsonInfo(pkgJsonPath string) *packagejson.InfoCacheEntry {
	if e, ok := p.packageJsons[pkgJsonPath]; ok {
		p.Calls[callGetPackageJsonInfo]++
		return e
	}
	v := lookup(p, callGetPackageJsonInfo, p.fixture.PackageJson, pkgJsonPath)
	var e *packagejson.InfoCacheEntry
	if v != nil {
		e = v.entry()
	}
	p.packageJsons[pkgJsonPath] = e
	return e
}

func (p *FixtureProgram) GetSymlinkCache() *symlinks.KnownSymlinks {
	p.scalar(callSymlinkCache)
	return p.symlinks
}

func (p *FixtureProgram) GetRedirectTargets(path tspath.Path) []string {
	return lookup(p, callGetRedirectTargets, p.fixture.RedirectTarget, string(path))
}

func (p *FixtureProgram) GetSourceOfProjectReferenceIfOutputIncluded(file ast.HasFileName) string {
	return lookup(p, callGetSourceOfProjectReferenceIfOutputIncluded, p.fixture.SourceOfOutput, pathKey(file))
}

func (p *FixtureProgram) GetNearestAncestorDirectoryWithPackageJson(dirname string) string {
	return lookup(p, callGetNearestAncestorDirectoryWithPackageJson, p.fixture.NearestPackage, dirname)
}

func (p *FixtureProgram) GetJSXRuntimeImportSpecifier(path tspath.Path) (string, store.Node) {
	reference := lookup(p, callGetJSXRuntimeImportSpecifier, p.fixture.JSXReference, string(path))
	return reference, p.jsxImports[path]
}

func (p *FixtureProgram) GetImportHelpersImportSpecifier(path tspath.Path) store.Node {
	p.Calls[callGetImportHelpersImportSpecifier]++
	return p.helperImports[path]
}

func (p *FixtureProgram) CommonSourceDirectory() string {
	p.scalar(callCommonSourceDirectory)
	return p.fixture.Scalars.CommonSourceDirectory
}

func (p *FixtureProgram) ContentMapperExtensions() []string {
	p.scalar(callContentMapperExtensions)
	return p.fixture.Scalars.ContentMapperExtensions
}

func (p *FixtureProgram) GetGlobalTypingsCacheLocation() string {
	p.scalar(callGlobalTypingsLocation)
	return p.fixture.Scalars.GlobalTypingsLocation
}

func (p *FixtureProgram) UseCaseSensitiveFileNames() bool {
	p.scalar(callUseCaseSensitiveFileNames)
	return p.fixture.Scalars.UseCaseSensitiveFileNames
}

func (p *FixtureProgram) GetCurrentDirectory() string {
	p.scalar(callCurrentDirectory)
	return p.fixture.Scalars.CurrentDirectory
}
