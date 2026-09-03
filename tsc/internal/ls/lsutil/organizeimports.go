package lsutil

import (
	"cmp"
	"github.com/microsoft/TypeScript/tsc/internal/ast"
	"github.com/microsoft/TypeScript/tsc/internal/core"
	"github.com/microsoft/TypeScript/tsc/internal/stringutil"
	"github.com/microsoft/TypeScript/tsc/internal/tspath"
	"golang.org/x/text/unicode/norm"
	"math"
	"strings"
	"unicode"
	"unicode/utf8"
)

func FilterImportDeclarations(statements []ast.Handle) []ast.Handle {
	return core.Filter(statements, func(stmt ast.Handle) bool {
		return stmt.Kind == ast.KindImportDeclaration
	})
}

func GetDetectionLists(preferences UserPreferences) (comparersToTest []func(a, b string) int, typeOrdersToTest []OrganizeImportsTypeOrder) {
	if preferences.OrganizeImportsSort != OrganizeImportsSortAuto {
		comparersToTest = []func(a, b string) int{getOrganizeImportsPresetStringComparer(preferences.OrganizeImportsSort)}
	} else if !preferences.OrganizeImportsIgnoreCase.IsUnknown() {
		comparersToTest = []func(a, b string) int{getOrganizeImportsStringComparer(preferences, preferences.OrganizeImportsIgnoreCase.IsTrue())}
	} else {
		comparersToTest = []func(a, b string) int{getOrganizeImportsStringComparer(preferences, true), getOrganizeImportsStringComparer(preferences, false)}
	}
	if preferences.OrganizeImportsTypeOrder != OrganizeImportsTypeOrderAuto {
		typeOrdersToTest = []OrganizeImportsTypeOrder{preferences.OrganizeImportsTypeOrder}
	} else {
		typeOrdersToTest = []OrganizeImportsTypeOrder{OrganizeImportsTypeOrderLast, OrganizeImportsTypeOrderInline, OrganizeImportsTypeOrderFirst}
	}
	return comparersToTest, typeOrdersToTest
}
func ResolveOrganizeImportsSort(preferences UserPreferences) OrganizeImportsSort {
	if preferences.OrganizeImportsSort != OrganizeImportsSortAuto {
		return preferences.OrganizeImportsSort
	}
	if preferences.OrganizeImportsCollation == OrganizeImportsCollationUnicode {
		switch preferences.OrganizeImportsIgnoreCase {
		case core.TSTrue:
			return OrganizeImportsSortNaturalIgnoreCase
		case core.TSFalse:
			return OrganizeImportsSortNatural
		default:
			return OrganizeImportsSortAuto
		}
	}
	switch preferences.OrganizeImportsIgnoreCase {
	case core.TSTrue:
		return OrganizeImportsSortOrdinalIgnoreCase
	case core.TSFalse:
		return OrganizeImportsSortOrdinal
	default:
		return OrganizeImportsSortAuto
	}
}
func getOrganizeImportsOrdinalStringComparer(ignoreCase bool) func(a, b string) int {
	if ignoreCase {
		return stringutil.CompareStringsCaseInsensitiveEslintCompatible
	}
	return stringutil.CompareStringsCaseSensitive
}
func getOrganizeImportsNaturalStringComparer(caseSensitive bool) func(a, b string) int {
	return func(a, b string) int {
		return compareOrganizeImportsNaturalStrings(a, b, caseSensitive)
	}
}
func getOrganizeImportsUnicodeStringComparer(ignoreCase bool, preferences UserPreferences) func(a, b string) int {
	caseFirst := preferences.OrganizeImportsCaseFirst
	numeric := preferences.OrganizeImportsNumericCollation.IsTrue()
	accents := !preferences.OrganizeImportsAccentCollation.IsFalse()
	return func(a, b string) int {
		return compareOrganizeImportsUnicodeStrings(a, b, ignoreCase, caseFirst, numeric, accents)
	}
}
func compareOrganizeImportsNaturalStrings(a string, b string, caseSensitive bool) int {
	if cmp := compareStringsNumeric(naturalCollationKey(a), naturalCollationKey(b)); cmp != 0 {
		return cmp
	}
	if caseSensitive {
		if cmp := compareOrganizeImportsCaseUpperFirst(a, b); cmp != 0 {
			return cmp
		}
	}
	return strings.Compare(a, b)
}
func compareOrganizeImportsUnicodeStrings(a string, b string, ignoreCase bool, caseFirst OrganizeImportsCaseFirst, numeric bool, accents bool) int {
	if cmp := compareOrganizeImportsUnicodeKeys(naturalCollationKey(a), naturalCollationKey(b), numeric); cmp != 0 {
		return cmp
	}
	if accents {
		if cmp := compareOrganizeImportsUnicodeKeys(strings.ToLower(a), strings.ToLower(b), numeric); cmp != 0 {
			return cmp
		}
	}
	if !ignoreCase {
		if cmp := compareOrganizeImportsCase(a, b, caseFirst); cmp != 0 {
			return cmp
		}
	}
	return strings.Compare(a, b)
}
func naturalCollationKey(s string) string {
	return strings.ToLower(removeDiacritics(s))
}
func removeDiacritics(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.Is(unicode.Mn, r) {
			return -1
		}
		return r
	}, norm.NFD.String(s))
}
func compareOrganizeImportsUnicodeKeys(a string, b string, numeric bool) int {
	if numeric {
		return compareStringsNumeric(a, b)
	}
	return strings.Compare(a, b)
}
func compareStringsNumeric(a string, b string) int {
	for len(a) > 0 && len(b) > 0 {
		if isASCIIDigit(a[0]) && isASCIIDigit(b[0]) {
			aRunEnd := asciiDigitRunEnd(a)
			bRunEnd := asciiDigitRunEnd(b)
			if cmp := compareNumericText(a[:aRunEnd], b[:bRunEnd]); cmp != 0 {
				return cmp
			}
			a = a[aRunEnd:]
			b = b[bRunEnd:]
			continue
		}
		aRune, aSize := utf8.DecodeRuneInString(a)
		bRune, bSize := utf8.DecodeRuneInString(b)
		if aRune != bRune {
			return cmp.Compare(aRune, bRune)
		}
		a = a[aSize:]
		b = b[bSize:]
	}
	return cmp.Compare(len(a), len(b))
}
func isASCIIDigit(ch byte) bool {
	return ch >= '0' && ch <= '9'
}
func asciiDigitRunEnd(s string) int {
	i := 0
	for i < len(s) && isASCIIDigit(s[i]) {
		i++
	}
	return i
}
func compareNumericText(a string, b string) int {
	aDigits := strings.TrimLeft(a, "0")
	bDigits := strings.TrimLeft(b, "0")
	if aDigits == "" {
		aDigits = "0"
	}
	if bDigits == "" {
		bDigits = "0"
	}
	if len(aDigits) != len(bDigits) {
		return cmp.Compare(len(aDigits), len(bDigits))
	}
	if cmp := strings.Compare(aDigits, bDigits); cmp != 0 {
		return cmp
	}
	return strings.Compare(a, b)
}
func compareOrganizeImportsCaseUpperFirst(a string, b string) int {
	return compareOrganizeImportsCase(a, b, OrganizeImportsCaseFirstUpper)
}
func compareOrganizeImportsCase(a string, b string, caseFirst OrganizeImportsCaseFirst) int {
	aRunes := []rune(a)
	bRunes := []rune(b)
	minLen := min(len(aRunes), len(bRunes))
	for i := range minLen {
		aUpper := unicode.IsUpper(aRunes[i])
		bUpper := unicode.IsUpper(bRunes[i])
		if aUpper != bUpper {
			switch caseFirst {
			case OrganizeImportsCaseFirstUpper:
				if aUpper {
					return -1
				}
				return 1
			case OrganizeImportsCaseFirstLower:
				if !aUpper {
					return -1
				}
				return 1
			default:
				if aUpper {
					return 1
				}
				return -1
			}
		}
	}
	return cmp.Compare(len(aRunes), len(bRunes))
}
func getOrganizeImportsPresetStringComparer(sort OrganizeImportsSort) func(a, b string) int {
	switch sort {
	case OrganizeImportsSortOrdinalIgnoreCase:
		return getOrganizeImportsOrdinalStringComparer(true)
	case OrganizeImportsSortNatural:
		return getOrganizeImportsNaturalStringComparer(true)
	case OrganizeImportsSortNaturalIgnoreCase:
		return getOrganizeImportsNaturalStringComparer(false)
	default:
		return getOrganizeImportsOrdinalStringComparer(false)
	}
}
func getOrganizeImportsStringComparer(preferences UserPreferences, ignoreCase bool) func(a, b string) int {
	if preferences.OrganizeImportsSort != OrganizeImportsSortAuto {
		return getOrganizeImportsPresetStringComparer(preferences.OrganizeImportsSort)
	}
	if preferences.OrganizeImportsCollation == OrganizeImportsCollationUnicode {
		return getOrganizeImportsUnicodeStringComparer(ignoreCase, preferences)
	}
	return getOrganizeImportsOrdinalStringComparer(ignoreCase)
}
func getModuleSpecifierExpression(declaration ast.Handle) ast.Handle {
	switch declaration.Kind {
	case ast.KindImportEqualsDeclaration:
		importEquals := declaration
		if importEquals.ModuleReference().Kind == ast.KindExternalModuleReference {
			return importEquals.ModuleReference().Expression()
		}
		return ast.Handle{}
	case ast.KindImportDeclaration:
		return declaration.ModuleSpecifier()
	case ast.KindVariableStatement:
		declarations := declaration.Store().ListSlice(declaration.VariableStatementDeclarationList().VariableDeclarationListDeclarations())
		if len(declarations) > 0 {
			initializer := declarations[0].Initializer()
			if !initializer.IsNil() && initializer.Kind == ast.KindCallExpression {
				callExpr := initializer
				if len(callExpr.Arguments()) > 0 {
					return callExpr.Arguments()[0]
				}
			}
		}
		return ast.Handle{}
	default:
		return ast.Handle{}
	}
}

func GetExternalModuleName(specifier ast.Handle) string {
	if !specifier.IsNil() && ast.IsStringLiteralLike(specifier) {
		return specifier.Text()
	}
	return ""
}

func CompareModuleSpecifiers(m1 ast.Handle, m2 ast.Handle, comparer func(a, b string) int) int {
	name1 := GetExternalModuleName(m1)
	name2 := GetExternalModuleName(m2)
	if cmp := core.CompareBooleans(name1 == "", name2 == ""); cmp != 0 {
		return cmp
	}
	if cmp := core.CompareBooleans(tspath.IsExternalModuleNameRelative(name1), tspath.IsExternalModuleNameRelative(name2)); cmp != 0 {
		return cmp
	}
	return comparer(name1, name2)
}
func compareImportKind(s1 ast.Handle, s2 ast.Handle) int {
	return cmp.Compare(getImportKindOrder(s1), getImportKindOrder(s2))
}

const (
	importKindOrderSideEffect   = 0
	importKindOrderTypeOnly     = 1
	importKindOrderNamespace    = 2
	importKindOrderDefault      = 3
	importKindOrderNamed        = 4
	importKindOrderImportEquals = 5
	importKindOrderRequire      = 6
	importKindOrderUnknown      = 7
)

func getImportKindOrder(s1 ast.Handle) int {
	switch s1.Kind {
	case ast.KindImportDeclaration:
		importDecl := s1
		if importDecl.ImportClause().IsNil() {
			return importKindOrderSideEffect
		}
		importClause := importDecl.ImportClause()
		if importClause.IsTypeOnly() {
			return importKindOrderTypeOnly
		}
		if !importClause.NamedBindings().IsNil() && importClause.NamedBindings().Kind == ast.KindNamespaceImport {
			return importKindOrderNamespace
		}
		if !importClause.Name().IsNil() {
			return importKindOrderDefault
		}
		return importKindOrderNamed
	case ast.KindImportEqualsDeclaration:
		return importKindOrderImportEquals
	case ast.KindVariableStatement:
		return importKindOrderRequire
	default:
		return importKindOrderUnknown
	}
}

func CompareImportsOrRequireStatements(s1 ast.Handle, s2 ast.Handle, comparer func(a, b string) int) int {
	if cmp := CompareModuleSpecifiers(getModuleSpecifierExpression(s1), getModuleSpecifierExpression(s2), comparer); cmp != 0 {
		return cmp
	}
	return compareImportKind(s1, s2)
}
func compareImportOrExportSpecifiers(s1 ast.Handle, s2 ast.Handle, comparer func(a, b string) int, preferences UserPreferences) int {
	typeOrder := preferences.OrganizeImportsTypeOrder
	s1Name := s1.Name().Text()
	s2Name := s2.Name().Text()
	switch typeOrder {
	case OrganizeImportsTypeOrderFirst:
		if cmp := core.CompareBooleans(s2.IsTypeOnly(), s1.IsTypeOnly()); cmp != 0 {
			return cmp
		}
		return comparer(s1Name, s2Name)
	case OrganizeImportsTypeOrderInline:
		return comparer(s1Name, s2Name)
	default:
		if cmp := core.CompareBooleans(s1.IsTypeOnly(), s2.IsTypeOnly()); cmp != 0 {
			return cmp
		}
		return comparer(s1Name, s2Name)
	}
}

func GetNamedImportSpecifierComparer(preferences UserPreferences, comparer func(a, b string) int) func(s1, s2 ast.Handle) int {
	if comparer == nil {
		ignoreCase := false
		if !preferences.OrganizeImportsIgnoreCase.IsUnknown() {
			ignoreCase = preferences.OrganizeImportsIgnoreCase.IsTrue()
		}
		comparer = getOrganizeImportsStringComparer(preferences, ignoreCase)
	}
	return func(s1, s2 ast.Handle) int {
		return compareImportOrExportSpecifiers(s1, s2, comparer, preferences)
	}
}

func GetImportSpecifierInsertionIndex(sortedImports []ast.Handle, newImport ast.Handle, comparer func(s1, s2 ast.Handle) int) int {
	return core.FirstResult(core.BinarySearchUniqueFunc(sortedImports, func(mid int, value ast.Handle) int {
		return comparer(value, newImport)
	}))
}

func GetImportDeclarationInsertIndex(sortedImports []ast.Handle, newImport ast.Handle, comparer func(a, b ast.Handle) int) int {
	return core.FirstResult(core.BinarySearchUniqueFunc(sortedImports, func(mid int, value ast.Handle) int {
		return comparer(value, newImport)
	}))
}

func GetOrganizeImportsStringComparerWithDetection(originalImportDecls []ast.Handle, preferences UserPreferences) (comparer func(a, b string) int, isSorted bool) {
	result, sorted := DetectModuleSpecifierCaseBySort([][]ast.Handle{originalImportDecls}, getComparers(preferences))
	return result, sorted
}
func getComparers(preferences UserPreferences) []func(a string, b string) int {
	if preferences.OrganizeImportsSort != OrganizeImportsSortAuto || !preferences.OrganizeImportsIgnoreCase.IsUnknown() {
		ignoreCase := false
		if !preferences.OrganizeImportsIgnoreCase.IsUnknown() {
			ignoreCase = preferences.OrganizeImportsIgnoreCase.IsTrue()
		}
		return []func(a, b string) int{getOrganizeImportsStringComparer(preferences, ignoreCase)}
	}
	return []func(a, b string) int{getOrganizeImportsStringComparer(preferences, true), getOrganizeImportsStringComparer(preferences, false)}
}

type namedImportSortResult struct {
	namedImportComparer func(a, b string) int
	typeOrder           OrganizeImportsTypeOrder
	isSorted            bool
}

func DetectNamedImportOrganizationBySort(originalGroups []ast.Handle, comparersToTest []func(a, b string) int, typesToTest []OrganizeImportsTypeOrder) (comparer func(a, b string) int, typeOrder OrganizeImportsTypeOrder, found bool) {
	result := detectNamedImportOrganizationBySort(originalGroups, comparersToTest, typesToTest)
	if result == nil {
		return nil, OrganizeImportsTypeOrderLast, false
	}
	return result.namedImportComparer, result.typeOrder, true
}
func detectNamedImportOrganizationBySort(originalGroups []ast.Handle, comparersToTest []func(a, b string) int, typesToTest []OrganizeImportsTypeOrder) *namedImportSortResult {
	var bothNamedImports bool
	var importDeclsWithNamed []ast.Handle
	for _, imp := range originalGroups {
		if imp.ImportDeclarationImportClause().IsNil() {
			continue
		}
		clause := imp.ImportDeclarationImportClause()
		if clause.NamedBindings().IsNil() || clause.NamedBindings().Kind != ast.KindNamedImports {
			continue
		}
		namedImports := clause.NamedBindings()
		if len(namedImports.Elements()) == 0 {
			continue
		}
		if !bothNamedImports {
			hasTypeOnly := false
			hasRegular := false
			for _, elem := range namedImports.Elements() {
				if elem.IsTypeOnly() {
					hasTypeOnly = true
				} else {
					hasRegular = true
				}
			}
			if hasTypeOnly && hasRegular {
				bothNamedImports = true
			}
		}
		importDeclsWithNamed = append(importDeclsWithNamed, imp)
	}
	if len(importDeclsWithNamed) == 0 {
		return nil
	}
	namedImportsByDecl := make([][]ast.Handle, 0, len(importDeclsWithNamed))
	for _, imp := range importDeclsWithNamed {
		clause := imp.ImportDeclarationImportClause()
		namedImports := clause.NamedBindings()
		namedImportsByDecl = append(namedImportsByDecl, namedImports.Elements())
	}
	if !bothNamedImports || len(typesToTest) == 0 {
		namesList := make([][]string, len(namedImportsByDecl))
		for i, imports := range namedImportsByDecl {
			names := make([]string, len(imports))
			for j, imp := range imports {
				names[j] = imp.Name().Text()
			}
			namesList[i] = names
		}
		sortState := detectCaseSensitivityBySort(namesList, comparersToTest)
		typeOrder := OrganizeImportsTypeOrderLast
		if len(typesToTest) == 1 {
			typeOrder = typesToTest[0]
		}
		return &namedImportSortResult{namedImportComparer: sortState.comparer, typeOrder: typeOrder, isSorted: sortState.isSorted}
	}
	bestDiff := map[OrganizeImportsTypeOrder]int{OrganizeImportsTypeOrderFirst: math.MaxInt, OrganizeImportsTypeOrderLast: math.MaxInt, OrganizeImportsTypeOrderInline: math.MaxInt}
	bestComparer := map[OrganizeImportsTypeOrder]func(a, b string) int{OrganizeImportsTypeOrderFirst: comparersToTest[0], OrganizeImportsTypeOrderLast: comparersToTest[0], OrganizeImportsTypeOrderInline: comparersToTest[0]}
	for _, curComparer := range comparersToTest {
		currDiff := map[OrganizeImportsTypeOrder]int{OrganizeImportsTypeOrderFirst: 0, OrganizeImportsTypeOrderLast: 0, OrganizeImportsTypeOrderInline: 0}
		for _, importDecl := range namedImportsByDecl {
			for _, typeOrder := range typesToTest {
				prefs := UserPreferences{OrganizeImportsTypeOrder: typeOrder}
				diff := measureSortedness(importDecl, func(n1, n2 ast.Handle) int {
					return compareImportOrExportSpecifiers(n1, n2, curComparer, prefs)
				})
				currDiff[typeOrder] = currDiff[typeOrder] + diff
			}
		}
		for _, typeOrder := range typesToTest {
			if currDiff[typeOrder] < bestDiff[typeOrder] {
				bestDiff[typeOrder] = currDiff[typeOrder]
				bestComparer[typeOrder] = curComparer
			}
		}
	}
	for _, bestTypeOrder := range typesToTest {
		isBest := true
		for _, testTypeOrder := range typesToTest {
			if bestDiff[testTypeOrder] < bestDiff[bestTypeOrder] {
				isBest = false
				break
			}
		}
		if isBest {
			return &namedImportSortResult{namedImportComparer: bestComparer[bestTypeOrder], typeOrder: bestTypeOrder, isSorted: bestDiff[bestTypeOrder] == 0}
		}
	}
	return &namedImportSortResult{namedImportComparer: bestComparer[OrganizeImportsTypeOrderLast], typeOrder: OrganizeImportsTypeOrderLast, isSorted: bestDiff[OrganizeImportsTypeOrderLast] == 0}
}

type caseSensitivityDetectionResult struct {
	comparer func(a, b string) int
	isSorted bool
}

func DetectModuleSpecifierCaseBySort(importDeclsByGroup [][]ast.Handle, comparersToTest []func(a, b string) int) (comparer func(a, b string) int, isSorted bool) {
	moduleSpecifiersByGroup := make([][]string, 0, len(importDeclsByGroup))
	for _, importGroup := range importDeclsByGroup {
		moduleNames := make([]string, 0, len(importGroup))
		for _, decl := range importGroup {
			if expr := getModuleSpecifierExpression(decl); !expr.IsNil() {
				moduleNames = append(moduleNames, GetExternalModuleName(expr))
			} else {
				moduleNames = append(moduleNames, "")
			}
		}
		moduleSpecifiersByGroup = append(moduleSpecifiersByGroup, moduleNames)
	}
	result := detectCaseSensitivityBySort(moduleSpecifiersByGroup, comparersToTest)
	return result.comparer, result.isSorted
}
func detectCaseSensitivityBySort(originalGroups [][]string, comparersToTest []func(a, b string) int) caseSensitivityDetectionResult {
	var bestComparer func(a, b string) int
	bestDiff := math.MaxInt
	for _, curComparer := range comparersToTest {
		diffOfCurrentComparer := 0
		for _, listToSort := range originalGroups {
			if len(listToSort) <= 1 {
				continue
			}
			diff := measureSortedness(listToSort, curComparer)
			diffOfCurrentComparer += diff
		}
		if diffOfCurrentComparer < bestDiff {
			bestDiff = diffOfCurrentComparer
			bestComparer = curComparer
		}
	}
	if bestComparer == nil && len(comparersToTest) > 0 {
		bestComparer = comparersToTest[0]
	}
	return caseSensitivityDetectionResult{comparer: bestComparer, isSorted: bestDiff == 0}
}
func measureSortedness[T any](arr []T, comparer func(a, b T) int) int {
	i := 0
	for j := range len(arr) - 1 {
		if comparer(arr[j], arr[j+1]) > 0 {
			i++
		}
	}
	return i
}

func GetNamedImportSpecifierComparerWithDetection(importDecl ast.Handle, sourceFile *ast.SourceFile, preferences UserPreferences) (specifierComparer func(s1, s2 ast.Handle) int, isSorted core.Tristate) {
	comparersToTest, typeOrdersToTest := GetDetectionLists(preferences)
	var importStmt ast.Handle
	if importDecl.Kind == ast.KindImportDeclaration {
		importStmt = importDecl
	}
	specifierComparer = GetNamedImportSpecifierComparer(preferences, comparersToTest[0])
	isSorted = core.TSUnknown
	if (ResolveOrganizeImportsSort(preferences) == OrganizeImportsSortAuto || preferences.OrganizeImportsTypeOrder == OrganizeImportsTypeOrderAuto) && !importStmt.IsNil() {
		detectFromDecl := detectNamedImportOrganizationBySort([]ast.Handle{importStmt}, comparersToTest, typeOrdersToTest)
		if detectFromDecl != nil {
			isSorted = core.BoolToTristate(detectFromDecl.isSorted)
			specifierComparer = GetNamedImportSpecifierComparer(UserPreferences{OrganizeImportsTypeOrder: detectFromDecl.typeOrder}, detectFromDecl.namedImportComparer)
		} else if sourceFile != nil {
			allImports := FilterImportDeclarations(sourceFile.ParseRoot().Statements())
			detectFromFile := detectNamedImportOrganizationBySort(allImports, comparersToTest, typeOrdersToTest)
			if detectFromFile != nil {
				isSorted = core.BoolToTristate(detectFromFile.isSorted)
				specifierComparer = GetNamedImportSpecifierComparer(UserPreferences{OrganizeImportsTypeOrder: detectFromFile.typeOrder}, detectFromFile.namedImportComparer)
			}
		}
	}
	return specifierComparer, isSorted
}
