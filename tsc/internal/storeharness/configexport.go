package storeharness

import (
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/tsoptions"
)

// The stage 2 enumeration and preparation (configprep.go) for the native
// equivalence of 7e (storeclitest): the cases with their ids, and each case's
// input values, from which each side builds a program of its own.

// Case is a configuration of a compiler or conformance test.
type Case = configCase

// InputError is a test whose configurations are not enumerated.
type InputError = inputError

// Outcome is how the runner's preparation of a case ended: Kind "" when it
// returned.
type Outcome = tOutcome

// Descriptor is a prepared case's input values.
type Descriptor = configDescriptor

// EnumerateConfigurations are every configuration of the compiler and
// conformance tests, in the runners' order, the inputs that fail to
// enumerate, and the number of test files.
func EnumerateConfigurations() ([]*Case, []*InputError, int, error) {
	return enumerateConfigurations(runnerSuites)
}

// PrepareCase is the runner's preparation of c.
func PrepareCase(c *Case) (*Descriptor, Outcome) { return prepareCase(c) }

// FileMap is the case's file system: a new map, for vfstest.FromMap.
func (d *configDescriptor) FileMap() map[string]any { return d.fileMap() }

// UseCaseSensitiveFileNames is the case's file system's.
func (d *configDescriptor) UseCaseSensitiveFileNames() bool {
	return d.harnessOptions.UseCaseSensitiveFileNames
}

// CurrentDirectory is the program's cwd.
func (d *configDescriptor) CurrentDirectory() string { return d.currentDirectory }

// Options are the options the runner gives the program: a copy.
func (d *configDescriptor) Options() *core.CompilerOptions { return d.options.Clone() }

// CommandLine is the program's config with options (Options, changed).
func (d *configDescriptor) CommandLine(options *core.CompilerOptions) *tsoptions.ParsedCommandLine {
	return d.commandLine(options)
}
